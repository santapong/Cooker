package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/source/gitea"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/source/gitlab"
	"github.com/santapong/cooker/internal/source/gittransport"
	"github.com/santapong/cooker/internal/store"
)

// SourceWebhook authenticates before recording any idempotency identity. It
// intentionally bypasses the legacy process-local idempotency middleware.
func (h *Handler) SourceWebhook(c *gin.Context) {
	if h.Automation == nil || h.SourceConnections == nil {
		c.JSON(503, gin.H{"error": "durable source automation is unavailable"})
		return
	}
	conn, err := h.SourceConnections.Store.Get(c.Request.Context(), c.Param("id"))
	if err != nil || !conn.Enabled {
		c.JSON(404, gin.H{"error": "source connection is unavailable"})
		return
	}
	body, ok := readWebhookBody(c)
	if !ok {
		return
	}
	secret, err := h.SourceConnections.WebhookSecret(conn)
	if err != nil {
		c.JSON(503, gin.H{"error": "webhook credential is unavailable"})
		return
	}
	deliveryID, event, err := authenticateSourceWebhook(conn, secret, body, c.Request.Header, time.Now())
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "webhook authentication failed"})
		return
	}
	journal, journalOK := h.Automation.Triggers.Store.(store.WebhookAttemptStore)
	if !journalOK {
		c.JSON(503, gin.H{"error": "authenticated delivery audit is unavailable"})
		return
	}
	validDeliveryID := store.SafeWebhookTransportID(deliveryID)
	attempt := &model.WebhookAttempt{ID: uuid.NewString(), Provider: conn.Provider, ConnectionID: conn.ID, Status: "received", ReasonCode: "processing"}
	if validDeliveryID {
		attempt.ProviderDeliveryID = deliveryID
	}
	if err := journal.BeginWebhookAttempt(c.Request.Context(), attempt); err != nil {
		c.JSON(503, gin.H{"error": "cannot begin authenticated delivery audit; retry the original delivery"})
		return
	}
	// A durable received row survives cancellation or crashes. Finish uses a
	// short independent deadline; if it fails, never report a confirmed audit
	// result. Retrying preserves the existing authenticated inbox identity.
	reply := func(httpCode int, body any, status, reasonCode, linkedDelivery string) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 3*time.Second)
		defer cancel()
		if err := journal.FinishWebhookAttempt(ctx, attempt.ID, status, reasonCode, linkedDelivery); err != nil {
			c.JSON(503, gin.H{"error": "delivery audit completion is unconfirmed; retry the original provider identity"})
			return
		}
		c.JSON(httpCode, body)
	}
	if !validDeliveryID {
		reply(400, gin.H{"error": "provider delivery identity is required"}, "rejected", "invalid_delivery_identity", "")
		return
	}
	// Authenticate every retry before consulting the durable identity. Tag
	// normalization depends on provider state; reuse the saved result even if
	// the tag subsequently moved or the provider is temporarily unavailable.
	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])
	saved, lookupErr := h.Automation.Triggers.Store.GetDelivery(c.Request.Context(), service.TriggerDeliveryID(conn.Provider, conn.ID, deliveryID))
	if lookupErr == nil {
		if saved.Event.PayloadHash != payloadHash {
			reply(409, gin.H{"error": "delivery identity was reused with different authenticated content"}, "rejected", "delivery_identity_conflict", saved.ID)
			return
		}
		reply(http.StatusAccepted, gin.H{"delivery": saved, "duplicate": true, "status": "recorded"}, "duplicate", "already_recorded", saved.ID)
		return
	}
	if !errors.Is(lookupErr, store.ErrNotFound) {
		reply(503, gin.H{"error": "delivery history is temporarily unavailable"}, "failed", "history_unavailable", "")
		return
	}
	if event != "push" && event != "Push Hook" && event != "Tag Push Hook" {
		reply(200, gin.H{"ignored": "event does not trigger deployment"}, "ignored", "unsupported_event", "")
		return
	}
	var payload struct {
		Ref        string `json:"ref"`
		Before     string `json:"before"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
		Project struct {
			ID   int64  `json:"id"`
			Path string `json:"path_with_namespace"`
		} `json:"project"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		reply(400, gin.H{"error": "invalid webhook event"}, "rejected", "invalid_json", "")
		return
	}
	repo, id := payload.Repository.FullName, payload.Repository.ID
	if conn.Provider == "gitlab" {
		repo, id = payload.Project.Path, payload.Project.ID
	}
	if !conn.Allows(repo) || id <= 0 {
		reply(403, gin.H{"error": "repository is not allowed by this connection"}, "rejected", "repository_not_allowed", "")
		return
	}
	if conn.Provider == "github" && conn.InstallationID != 0 && payload.Installation.ID != conn.InstallationID {
		reply(403, gin.H{"error": "GitHub installation does not match"}, "rejected", "installation_mismatch", "")
		return
	}
	u, _ := url.Parse(conn.Origin)
	e := model.VerifiedGitEvent{Source: model.GitSourceIdentity{Provider: conn.Provider, ConnectionID: conn.ID, Host: u.Host, RepositoryID: strconv.FormatInt(id, 10), Repository: repo}, DeliveryID: deliveryID, Event: "push", FullRef: payload.Ref, BeforeSHA: payload.Before, CommitSHA: payload.After, Deleted: payload.Deleted || payload.After == strings.Repeat("0", 40), PayloadHash: payloadHash}
	if err := service.ValidateVerifiedGitEvent(e); err != nil {
		reply(400, gin.H{"error": "invalid webhook event"}, "rejected", "invalid_event", "")
		return
	}
	// A tag webhook may name the annotated object instead of its commit.
	// Resolve only on this approved connection and require that the signed
	// after value identifies the returned commit/object; never substitute a
	// newer tag's different source. The resulting identity is saved once.
	if !e.Deleted && strings.HasPrefix(e.FullRef, "refs/tags/") {
		if h.Automation.Source == nil {
			reply(503, gin.H{"error": "tag source verification is unavailable"}, "failed", "tag_source_unavailable", "")
			return
		}
		work, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()
		identity, err := h.Automation.Source.Identity(work, conn, repo)
		if err != nil || !identity.SameRepository(e.Source) {
			reply(409, gin.H{"error": "tag repository identity could not be verified"}, "rejected", "tag_identity_unverified", "")
			return
		}
		rev, err := h.Automation.Source.Revision(work, conn, repo, e.FullRef)
		if err != nil {
			reply(503, gin.H{"error": "tag revision could not be verified; retry this delivery"}, "failed", "tag_revision_unavailable", "")
			return
		}
		if rev.FullRef != e.FullRef || gittransport.ValidateCommit(rev.Commit) != nil || gittransport.ValidateCommit(rev.Object) != nil || (payload.After != rev.Commit && payload.After != rev.Object) {
			reply(409, gin.H{"error": "tag changed before acceptance; check its current revision before deploying"}, "rejected", "tag_changed", "")
			return
		}
		e.CommitSHA, e.ObjectSHA = rev.Commit, rev.Object
	}
	d, duplicate, err := h.Automation.Triggers.AcceptVerified(c.Request.Context(), e)
	if err != nil {
		code := 503
		if errors.Is(err, store.ErrDeliveryConflict) {
			// Two authenticated retries may both normalize before either inserts.
			// The first committed normalization remains authoritative for this body.
			saved, readErr := h.Automation.Triggers.Store.GetDelivery(c.Request.Context(), service.TriggerDeliveryID(conn.Provider, conn.ID, deliveryID))
			if readErr == nil && saved.Event.PayloadHash == payloadHash {
				reply(http.StatusAccepted, gin.H{"delivery": saved, "duplicate": true, "status": "recorded"}, "duplicate", "already_recorded", saved.ID)
				return
			}
			code = 409
		}
		status, reason := "uncertain", "admission_unconfirmed"
		if code == 409 {
			status = "rejected"
			reason = "delivery_identity_conflict"
		}
		reply(code, gin.H{"error": "delivery could not be accepted; retry with its original provider identity"}, status, reason, "")
		return
	}
	status, reason := "accepted", "admitted"
	if duplicate {
		status = "duplicate"
		reason = "already_recorded"
	}
	reply(http.StatusAccepted, gin.H{"delivery": d, "duplicate": duplicate, "status": "recorded"}, status, reason, d.ID)
}

func authenticateSourceWebhook(c *connection.Connection, secret, body []byte, h http.Header, now time.Time) (string, string, error) {
	switch c.Provider {
	case "github":
		return h.Get("X-GitHub-Delivery"), h.Get("X-GitHub-Event"), github.VerifySignature(secret, body, h.Get("X-Hub-Signature-256"))
	case "gitea":
		return h.Get("X-Gitea-Delivery"), h.Get("X-Gitea-Event"), gitea.VerifySignature(secret, body, h.Get("X-Gitea-Signature"))
	case "gitlab":
		if c.WebhookAuthMode == "legacy_token" {
			id := h.Get("webhook-id")
			if id == "" {
				id = h.Get("X-Gitlab-Event-UUID")
			}
			return id, h.Get("X-Gitlab-Event"), gitlab.VerifyToken(secret, h.Get("X-Gitlab-Token"))
		}
		if c.WebhookAuthMode != "signing" {
			return "", "", fmt.Errorf("webhook mode is unavailable")
		}
		id, stamp := h.Get("webhook-id"), h.Get("webhook-timestamp")
		unix, err := strconv.ParseInt(stamp, 10, 64)
		if err != nil || unix < now.Unix()-300 || unix > now.Unix()+300 || id == "" {
			return "", "", fmt.Errorf("invalid signing timestamp")
		}
		key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(string(secret), "whsec_"))
		if err != nil || !strings.HasPrefix(string(secret), "whsec_") || len(key) != 32 {
			return "", "", fmt.Errorf("invalid signing token")
		}
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(id + "." + stamp + "."))
		_, _ = mac.Write(body)
		expected := mac.Sum(nil)
		for _, part := range strings.Fields(h.Get("webhook-signature")) {
			v, encoded, ok := strings.Cut(part, ",")
			if !ok || v != "v1" {
				continue
			}
			sig, err := base64.StdEncoding.DecodeString(encoded)
			if err == nil && hmac.Equal(expected, sig) {
				return id, h.Get("X-Gitlab-Event"), nil
			}
		}
	}
	return "", "", fmt.Errorf("invalid webhook signature")
}
