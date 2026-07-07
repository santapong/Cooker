package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/source/github"
)

// GitHubWebhook receives push events from GitHub and, for Apps with
// a matching repo+branch and AutoDeploy=true, enqueues a deploy.
//
// Route: POST /webhooks/github (unauthenticated — HMAC is the
// authentication).
func (h *Handler) GitHubWebhook(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	// GitHub's documented webhook payload cap is 25 MiB; we set a
	// hard 10 MiB limit because cooker only consumes push events
	// where realistic payloads are much smaller (a hundred-commit
	// push is around 200 KiB). Reading a 1 GiB body unbounded was
	// the simplest path to OOM-killing the pod.
	const maxWebhookBody = 10 << 20
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBody+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	if len(body) > maxWebhookBody {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": "payload exceeds limit",
		})
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
	if ev.IsBranchDelete() {
		// Branch was deleted (`after` is all zeros / `deleted: true`).
		// Nothing to deploy — and pushing through to GetByRepo would
		// surface a misleading "deploy queued" response.
		c.JSON(http.StatusOK, gin.H{"ignored": "branch delete"})
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

	h.triggerWebhookDeploy(c, app, "github", branch, ev.After)
}
