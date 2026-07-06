// Package handler contains HTTP handlers for Cooker's REST API. The
// Handler struct owns persistence through a *store.Store; router code
// constructs one instance and binds its methods as gin.HandlerFunc.
package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/kube"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/notifier"
	"github.com/santapong/cooker/internal/scheduler"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/templates"
)

// RunSpawner is a narrow interface implemented by server.RunCoordinator.
// Defining it here avoids a server→handler import cycle while letting
// tests inject a fake.
type RunSpawner interface {
	Spawn(ctx context.Context, runID string, work func(context.Context) error)
	// SpawnWithDeadline is Spawn with an explicit run deadline
	// (per-pipeline RunDeadline override); deadline <= 0 means "use
	// the cluster default".
	SpawnWithDeadline(ctx context.Context, runID string, deadline time.Duration, work func(context.Context) error)
}

// JobEnqueuer enqueues a pipeline-run job onto an async durable queue
// rather than running the executor inline. Wired in server.New only
// when COOKER_JOBQUEUE_ENABLED=true; nil otherwise. When nil,
// RunPipeline falls back to the inline RunSpawner path (the existing
// pre-Phase-1 behaviour). Implementations live in internal/service
// (see service.JobQueueEnqueuer).
type JobEnqueuer interface {
	EnqueueRun(ctx context.Context, pipelineID, runID string) error
}

// Handler owns the dependencies shared by request handlers.
type Handler struct {
	Store   *store.Store
	Codec   *crypto.Codec
	Secrets secrets.Manager
	// SecretsBackend names the configured secrets adapter ("database",
	// "keepsave", ...) for the connectivity-test response. Set by
	// server.New from COOKER_SECRETS_BACKEND.
	SecretsBackend string
	// Promotions persists run→environment promotions and approvals and
	// enforces PromotionPolicy.RequiredApprovers (HS26-05-01 / -08 / -14).
	// Set by server.New from the store; the promote/approve/env-status
	// handlers route through it. nil-safe: handlers fall back to 503 when
	// the store wasn't wired (defensive — server.New always sets it).
	Promotions *service.PromotionService
	// StageApprovals persists and resolves approval-gate stages
	// (StageTypeApproval). The stage approve/reject handlers route through
	// it; the executor uses the same service to open and poll the gate.
	// Set by server.New from the store (HS26-05-03). nil-safe: handlers
	// fall back to 503 when the store wasn't wired.
	StageApprovals *service.StageApprovalService
	// APITokens backs the personal-access / service-account token
	// endpoints (product-plan Tier 1). The create/list/delete handlers
	// route ownership + role-cap + no-self-replication decisions through
	// it. Set by server.New from the store; nil-safe (handlers return 503).
	APITokens *service.APITokenService
	// License backs the self-hosted licensing endpoints (M2 —
	// docs/launch/01-billing-monetization.md §4): GET /license (status +
	// entitlements) and the admin-only POST/DELETE /license. The
	// verify-on-install + degrade-to-Free logic lives in the service; the
	// handler stays thin. Set by server.New from the store + configured
	// public key; nil-safe (handlers return 503).
	License *service.LicenseService
	// MFAACRValues mirrors COOKER_OIDC_MFA_ACR_VALUES. The token-delete
	// handler uses it to step-up-gate an admin deleting ANOTHER user's
	// token (own-token deletes are exempt). Empty disables the gate.
	MFAACRValues []string
	AppDeployer  *service.AppDeployer
	// Canary orchestrates canary deployments (OR-1): the weighted split,
	// the live AppCanary state, and promote/abort. Set by server.New when
	// the configured deployer can split traffic (a WeightedDeployer);
	// nil-safe — the canary endpoints return 422/503 and DeployApp falls
	// back to a rolling deploy when it is nil.
	Canary *service.CanaryService
	// Triage backs the opt-in AI failure-triage endpoint (roadmap M4).
	// Set by server.New when COOKER_AI_TRIAGE_ENABLED=true; nil keeps
	// the route returning 503 and hides the frontend button via
	// /capabilities.
	Triage TriageRunner
	// AppDetector backs POST /apps/detect-build (New-App wizard recipe
	// suggestion). Set by server.New; nil returns 503.
	AppDetector *service.AppDetector
	// Feedback relays in-app feedback to GitHub issues (pure relay —
	// nothing is persisted). Set by server.New when
	// COOKER_FEEDBACK_GITHUB_TOKEN is non-empty; nil keeps the route
	// returning 503 and hides the frontend button via /capabilities.
	Feedback *service.FeedbackService
	// Hosts coordinates host-CRUD side-effects (writing SSH private
	// keys through secrets.Manager). Set by server.New; nil-safe in
	// dev when no secrets backend is configured (SSH host create/
	// update with a key body returns 503).
	Hosts *service.HostService
	// Registries / Clusters coordinate Settings-config CRUD
	// side-effects (writing the registry password / cluster kubeconfig
	// through secrets.Manager so the row carries only a reference;
	// HS26-05-04). Set by server.New; nil-safe in dev when no secrets
	// backend is configured — a Create that carries a credential then
	// returns 503, a credential-free Create persists via the plain store.
	Registries  *service.RegistryConfigService
	Clusters    *service.ClusterConfigService
	WSBroadcast func(channel string, data []byte)
	Executor    *service.Executor
	Runs        RunSpawner
	// Enqueuer routes pipeline runs through the durable async queue
	// (Phase-1 / A1). nil falls back to the inline Runs path. Set by
	// server.New when COOKER_JOBQUEUE_ENABLED=true.
	Enqueuer JobEnqueuer
	// Templates is the pipeline-template catalog (Phase-2 / F4). nil
	// returns 503 from the /templates endpoints; the rest of the API
	// is unaffected. Set by server.New when DATABASE_URL is non-empty.
	Templates templates.Store
	// Schedules is the cron-trigger catalog (Phase-2 / F2). nil returns
	// 503 from the /admin/schedules endpoints. Set by server.New when
	// COOKER_SCHEDULER_ENABLED=true.
	Schedules scheduler.Store
	// NotificationTargets is the notifier-target catalog (Phase-2 / F1).
	// nil returns 503 from the /admin/notification-targets endpoints.
	// Set by server.New when COOKER_JOBQUEUE_ENABLED=true (the
	// dispatcher only fires when the queue is running anyway).
	NotificationTargets notifier.TargetStore
	// Runtime inspects/tails the live container or pod backing a
	// deployed compose service (deployment-view runtime panel). Set by
	// server.New; nil returns 503 from the runtime endpoints.
	Runtime *service.RuntimeService
	// Kube is the read-only client-go client backing the Kubernetes
	// list/inspect endpoints. Set by server.New from the same kubeconfig
	// source as the ClientGo deployer; nil (or kube.ErrUnavailable from a
	// cluster that isn't reachable) returns 503 from the k8s read
	// endpoints. The write path (scale/restart/apply/delete) stays a stub
	// and does not use this field.
	Kube *kube.Client
	// CloudInventory backs the read-only cloud inventory & cost panel
	// (OR-2): GET /cloud/inventory, GET /cloud/costs, POST /cloud/refresh.
	// Set by server.New from COOKER_CLOUD_* config; nil (or a service with
	// no provider enabled) makes the endpoints return 200 with
	// enabled=false rather than an error. Read-only — never mutates any
	// cloud resource.
	CloudInventory CloudInventoryService
}

// New constructs a Handler bound to the given store. secs may be nil
// when no secrets backend is configured (dev mode with backend=database
// and no COOKER_SECRET_KEY set); the secret endpoints will return 503.
func New(s *store.Store, codec *crypto.Codec, secs secrets.Manager) *Handler {
	return &Handler{Store: s, Codec: codec, Secrets: secs}
}

// loadRunForPipeline fetches a run by runId and verifies it belongs
// to the given pipelineID. Mismatches return 404 (rather than 403)
// so we don't confirm to a probing caller whether a runId exists
// under a different pipeline. Returns nil + false if a response
// has already been written (caller should return immediately).
func (h *Handler) loadRunForPipeline(c *gin.Context, runID, pipelineID string) (*model.PipelineRun, bool) {
	run, err := h.Store.Runs.Get(c.Request.Context(), runID)
	if abortStoreErr(c, err, "run not found") {
		return nil, false
	}
	if run.PipelineID != pipelineID {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return nil, false
	}
	return run, true
}

// abortStoreErr maps common store errors to HTTP responses.
func abortStoreErr(c *gin.Context, err error, notFoundMsg string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": notFoundMsg})
		return true
	}
	if errors.Is(err, store.ErrConflict) {
		c.JSON(http.StatusConflict, gin.H{
			"error": "version conflict; refetch and retry",
		})
		return true
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	return true
}
