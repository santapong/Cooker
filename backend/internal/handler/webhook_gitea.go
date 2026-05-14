package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/source/gitea"
)

// GiteaWebhook receives push events from Gitea and, for Apps with a
// matching repo+branch and AutoDeploy=true, enqueues a deploy.
//
// Route: POST /webhooks/gitea
func (h *Handler) GiteaWebhook(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	body, ok := readWebhookBody(c)
	if !ok {
		return
	}

	if c.GetHeader("X-Gitea-Event") != "push" {
		c.JSON(http.StatusOK, gin.H{"ignored": c.GetHeader("X-Gitea-Event")})
		return
	}

	var ev gitea.PushEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse: " + err.Error()})
		return
	}
	branch := ev.Branch()
	if branch == "" {
		c.JSON(http.StatusOK, gin.H{"ignored": "non-branch push"})
		return
	}
	if ev.IsBranchDelete() {
		c.JSON(http.StatusOK, gin.H{"ignored": "branch delete"})
		return
	}

	app, err := h.Store.Apps.GetByRepo(c.Request.Context(), ev.FullName(), branch)
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}

	if len(app.WebhookSecret) == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "webhook not configured for this app"})
		return
	}
	secret, err := h.Codec.Open(app.WebhookSecret)
	if err != nil {
		slog.Warn("gitea webhook: failed to open secret", "app", app.ID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	if err := gitea.VerifySignature(secret, body, c.GetHeader("X-Gitea-Signature")); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "bad signature"})
		return
	}

	if !app.AutoDeploy {
		c.JSON(http.StatusOK, gin.H{"ignored": "autoDeploy disabled"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"appId":  app.ID,
		"commit": ev.After,
		"branch": branch,
		"status": "deploy queued",
	})
}
