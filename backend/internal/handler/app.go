package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/validate"
)

// validateAppInput rejects malformed App payloads.
func validateAppInput(a *model.App) error {
	if err := validate.Name("name", a.Name); err != nil {
		return err
	}
	if err := validate.Description("description", a.Description); err != nil {
		return err
	}
	if err := validate.GitHubRepo(a.GitHubRepo); err != nil {
		return err
	}
	if err := validate.GitRefName("branch", a.Branch); err != nil {
		return err
	}
	// Canary config (OR-1): normalise first so omitted optional fields get
	// defaults, then reject out-of-range values (weight outside 1–99, an
	// unknown strategy). Persisted in the same request, satisfying the
	// "new request field => migration" rule (apps.canary_config, mig 024).
	a.Canary = a.Canary.Normalize()
	if err := a.Canary.Validate(); err != nil {
		return err
	}
	return nil
}

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
	// Embed the live canary state (OR-1) when one is in flight so the
	// detail page renders the canary panel on first load without a second
	// round trip. Absent / errored canary state is simply omitted — it
	// must never block the app GET.
	if canary := h.activeCanary(c.Request.Context(), a.ID); canary != nil {
		redacted := a.Redact()
		c.JSON(http.StatusOK, appWithCanary{App: redacted, ActiveCanary: canary})
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
	if err := validateAppInput(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	if err := validateAppInput(&a); err != nil {
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
	id := c.Param("id")
	// Tear down an in-flight canary BEFORE deleting the app (PM26-07-07):
	// Abort scales the -canary Deployment down and collapses traffic back
	// to stable. It needs the app's namespace/name, which are gone once
	// the row is deleted (and the AppCanary row cascades away on Postgres),
	// so the split would otherwise be orphaned live in the cluster.
	// Best-effort: a teardown failure must not block the delete.
	if h.Canary != nil {
		if _, err := h.Canary.Abort(c.Request.Context(), id, "app deleted"); err != nil && !errors.Is(err, service.ErrNoActiveCanary) {
			slog.Warn("DeleteApp: canary teardown failed", "app", id, "err", err)
		}
	}
	if err := h.Store.Apps.Delete(c.Request.Context(), id); err != nil {
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
