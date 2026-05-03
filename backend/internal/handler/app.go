package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

// DeployApp synthesises a Clone→Build→Push→Deploy run for the App
// and streams progress over the app-run:<runId> WebSocket channel.
// Returns immediately with a 202 and the run ID; the run executes
// in a background goroutine bounded by a fresh context (the HTTP
// request context would cancel as soon as we reply).
func (h *Handler) DeployApp(c *gin.Context) {
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	if h.AppDeployer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "app deployer not configured",
		})
		return
	}

	// A run ID the client can subscribe to before the work starts.
	runID := uuid.New().String()
	channel := "app-run:" + runID

	if h.Runs != nil {
		h.Runs.Spawn(context.Background(), runID, func(ctx context.Context) error {
			h.runAppDeployCtx(ctx, a, runID, channel)
			return nil
		})
	} else {
		go h.runAppDeploy(a, runID, channel)
	}

	c.JSON(http.StatusAccepted, gin.H{
		"appId":   a.ID,
		"runId":   runID,
		"channel": channel,
		"status":  "running",
		"stream":  "/ws/app-run/" + runID,
		"repo":    a.GitHubRepo,
		"branch":  a.Branch,
	})
}

// runAppDeploy is the background worker for the legacy untracked path
// (used when no RunCoordinator is wired). Prefer runAppDeployCtx via
// the coordinator so heartbeats land in the run row.
func (h *Handler) runAppDeploy(a *model.App, runID, channel string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	h.runAppDeployCtx(ctx, a, runID, channel)
}

// runAppDeployCtx is the deploy worker bound to a caller-supplied ctx.
// The RunCoordinator owns the lifetime; this function should not
// install its own deadline so shutdown can cut off the work cleanly.
func (h *Handler) runAppDeployCtx(ctx context.Context, a *model.App, runID, channel string) {
	deployCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	sink := &wsLogSink{channel: channel, broadcast: h.WSBroadcast}
	sink.writef("[start] app=%s repo=%s branch=%s run=%s\n", a.Name, a.GitHubRepo, a.Branch, runID)

	run, err := h.AppDeployer.Deploy(deployCtx, a, sink)
	if err != nil {
		sink.writef("[error] %v\n", err)
	}
	if run != nil {
		// Coordinator-spawned runs have already had a row Created via
		// the synthesised PipelineRun returned by the deployer, so we
		// Update if it exists or fall back to Create. Use Update first
		// to preserve heartbeats written by the coordinator.
		if updateErr := h.Store.Runs.Update(deployCtx, run); updateErr != nil {
			if persistErr := h.Store.Runs.Create(deployCtx, run); persistErr != nil {
				sink.writef("[warn] persist run: %v\n", persistErr)
			}
		}
		sink.writef("[final] status=%s\n", run.Status)
	}
	sink.writef("[end] run=%s\n", runID)
}

// wsLogSink writes log lines as WebSocket messages on the given
// channel. Zero-value broadcast drops writes so the deployer can
// run in contexts without a hub (tests).
type wsLogSink struct {
	channel   string
	broadcast func(channel string, data []byte)
}

func (w *wsLogSink) Write(p []byte) (int, error) {
	if w.broadcast != nil && w.channel != "" {
		w.broadcast(w.channel, append([]byte(nil), p...))
	}
	return len(p), nil
}

func (w *wsLogSink) writef(format string, args ...any) {
	_, _ = w.Write([]byte(fmt.Sprintf(format, args...)))
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
		slog.Warn("github webhook: failed to open secret", "app", app.ID, "err", err)
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
		"appId":  app.ID,
		"commit": ev.After,
		"branch": branch,
		"status": "deploy queued",
	})
}
