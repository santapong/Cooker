package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/source/gitlab"
)

// maxWebhookBody bounds incoming webhook payload reads so a runaway
// or hostile sender can't OOM-kill the pod. 10 MiB is generous —
// realistic push events are well under 1 MiB.
const maxWebhookBody = 10 << 20

// GitLabWebhook receives push events from GitLab and, for Apps with
// a matching repo+branch and AutoDeploy=true, enqueues a deploy.
//
// Route: POST /webhooks/gitlab (unauthenticated — X-Gitlab-Token is
// the authentication). Mirrors GitHubWebhook's response semantics so
// the operator triage experience is consistent.
func (h *Handler) GitLabWebhook(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	body, ok := readWebhookBody(c)
	if !ok {
		return
	}

	if c.GetHeader("X-Gitlab-Event") != "Push Hook" {
		c.JSON(http.StatusOK, gin.H{"ignored": c.GetHeader("X-Gitlab-Event")})
		return
	}

	var ev gitlab.PushEvent
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
		slog.Warn("gitlab webhook: failed to open secret", "app", app.ID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	if err := gitlab.VerifyToken(secret, c.GetHeader("X-Gitlab-Token")); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "bad token"})
		return
	}

	if !app.AutoDeploy {
		c.JSON(http.StatusOK, gin.H{"ignored": "autoDeploy disabled"})
		return
	}

	// Converge on the manual-deploy path (see handler.triggerWebhookDeploy).
	h.triggerWebhookDeploy(c, app, "gitlab", branch, ev.After)
}

// readWebhookBody reads up to maxWebhookBody+1 bytes from c. Returns
// (body, true) on success; (nil, false) when an HTTP response has
// already been written and the caller should return.
func readWebhookBody(c *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBody+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return nil, false
	}
	if len(body) > maxWebhookBody {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": "payload exceeds limit",
		})
		return nil, false
	}
	return body, true
}
