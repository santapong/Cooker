// Package handler contains HTTP handlers for Cooker's REST API. The
// Handler struct owns persistence through a *store.Store; router code
// constructs one instance and binds its methods as gin.HandlerFunc.
package handler

import (
	"context"
	"errors"
	"net/http"

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
	Store       *store.Store
	Codec       *crypto.Codec
	Secrets     secrets.Manager
	AppDeployer *service.AppDeployer
	// Hosts coordinates host-CRUD side-effects (writing SSH private
	// keys through secrets.Manager). Set by server.New; nil-safe in
	// dev when no secrets backend is configured (SSH host create/
	// update with a key body returns 503).
	Hosts       *service.HostService
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
