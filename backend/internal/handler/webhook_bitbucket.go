package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/source/bitbucket"
)

// BitbucketWebhook receives push events from Bitbucket Server.
// Cloud-hosted Bitbucket doesn't sign webhook payloads (Atlassian
// expects IP allowlisting at the edge); only Server / Data Center
// is supported here.
//
// Route: POST /webhooks/bitbucket
func (h *Handler) BitbucketWebhook(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	body, ok := readWebhookBody(c)
	if !ok {
		return
	}

	if c.GetHeader("X-Event-Key") != "repo:refs_changed" {
		c.JSON(http.StatusOK, gin.H{"ignored": c.GetHeader("X-Event-Key")})
		return
	}

	var ev bitbucket.PushEvent
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
		slog.Warn("bitbucket webhook: failed to open secret", "app", app.ID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	if err := bitbucket.VerifySignature(secret, body, c.GetHeader("X-Hub-Signature")); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "bad signature"})
		return
	}

	if !app.AutoDeploy {
		c.JSON(http.StatusOK, gin.H{"ignored": "autoDeploy disabled"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"appId":  app.ID,
		"branch": branch,
		"status": "deploy queued",
	})
}
