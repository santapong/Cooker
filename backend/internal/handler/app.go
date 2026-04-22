package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cooker-ci/cooker/internal/auth"
	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/source/github"
)

// ListApps returns all apps with webhook secrets redacted.
func (h *Handler) ListApps(c *gin.Context) {
	apps, err := h.Store.Apps.List(c.Request.Context())
	if abortStoreErr(c, err, "apps not found") {
		return
	}
	out := make([]*model.App, 0, len(apps))
	for _, a := range apps {
		out = append(out, a.Redact())
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) GetApp(c *gin.Context) {
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	c.JSON(http.StatusOK, a.Redact())
}

func (h *Handler) CreateApp(c *gin.Context) {
	var a model.App
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if a.GitHubRepo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "githubRepo is required"})
		return
	}
	if a.Branch == "" {
		a.Branch = "main"
	}
	// Secrets are set via PUT /apps/:id/webhook — never on Create.
	a.WebhookSecret = nil
	a.ID = uuid.New().String()
	now := time.Now()
	a.CreatedAt, a.UpdatedAt = now, now
	if err := h.Store.Apps.Create(c.Request.Context(), &a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, a.Redact())
}

func (h *Handler) UpdateApp(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.Store.Apps.Get(c.Request.Context(), id)
	if abortStoreErr(c, err, "app not found") {
		return
	}
	var a model.App
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a.ID = id
	a.CreatedAt = existing.CreatedAt
	a.WebhookSecret = existing.WebhookSecret // keep; rotate via dedicated endpoint
	a.UpdatedAt = time.Now()
	if err := h.Store.Apps.Update(c.Request.Context(), &a); err != nil {
		if abortStoreErr(c, err, "app not found") {
			return
		}
	}
	c.JSON(http.StatusOK, a.Redact())
}

func (h *Handler) DeleteApp(c *gin.Context) {
	if err := h.Store.Apps.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "app not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// SetAppWebhookSecret rotates the HMAC secret GitHub will send as
// X-Hub-Signature-256. The value is sealed with the handler's codec
// before it hits the store. Admin only.
func (h *Handler) SetAppWebhookSecret(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	claims := auth.GetUser(c)
	if !auth.CanRevealSecret(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return
	}
	var req struct {
		Secret string `json:"secret" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	sealed, err := h.Codec.Seal([]byte(req.Secret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "seal: " + err.Error()})
		return
	}
	a.WebhookSecret = sealed
	a.UpdatedAt = time.Now()
	if err := h.Store.Apps.Update(c.Request.Context(), a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "rotated"})
}

// DeployApp triggers a pipeline run for an App. This is the "Deploy
// button" endpoint. The current implementation is a placeholder
// that records intent — the Clone→Build→Push→Deploy DAG
// synthesis lives in a follow-up once the source/github git clone
// is wired (roadmap Phase 3).
func (h *Handler) DeployApp(c *gin.Context) {
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"appId":     a.ID,
		"repo":      a.GitHubRepo,
		"branch":    a.Branch,
		"status":    "queued",
		"buildPlan": a.BuildPlan, // may be nil → detected at run time
	})
}

// GitHubWebhook receives push events from GitHub and, for Apps with
// a matching repo+branch and AutoDeploy=true, enqueues a deploy.
//
// Route: POST /webhooks/github (unauthenticated — HMAC is the
// authentication).
func (h *Handler) GitHubWebhook(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
		return
	}

	// GitHub only sends push events that matter for auto-deploy. We
	// look up the App by repository first to find the secret.
	if c.GetHeader("X-GitHub-Event") != "push" {
		c.JSON(http.StatusOK, gin.H{"ignored": c.GetHeader("X-GitHub-Event")})
		return
	}

	var ev github.PushEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse: " + err.Error()})
		return
	}
	branch := ev.Branch()
	if branch == "" {
		// Tag pushes etc. — ignore.
		c.JSON(http.StatusOK, gin.H{"ignored": "non-branch push"})
		return
	}

	app, err := h.Store.Apps.GetByRepo(c.Request.Context(), ev.Repository.FullName, branch)
	if err != nil {
		// No matching App — nothing to do, but don't leak that the
		// repo is unknown. Return 204 so GitHub keeps retrying on
		// real errors.
		c.Status(http.StatusNoContent)
		return
	}

	if len(app.WebhookSecret) == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "webhook not configured for this app"})
		return
	}
	secret, err := h.Codec.Open(app.WebhookSecret)
	if err != nil {
		log.Printf("github-webhook: open secret for app %s: %v", app.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	sig := c.GetHeader("X-Hub-Signature-256")
	if err := github.VerifySignature(secret, body, sig); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "bad signature"})
		return
	}

	if !app.AutoDeploy {
		c.JSON(http.StatusOK, gin.H{"ignored": "autoDeploy disabled"})
		return
	}

	// TODO: enqueue a real deploy (synthesise a Clone→Build→Push→Deploy run).
	c.JSON(http.StatusAccepted, gin.H{
		"appId":    app.ID,
		"commit":   ev.After,
		"branch":   branch,
		"status":   "deploy queued",
	})
}
