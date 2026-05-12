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
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

// RunSpawner is a narrow interface implemented by server.RunCoordinator.
// Defining it here avoids a server→handler import cycle while letting
// tests inject a fake.
type RunSpawner interface {
	Spawn(ctx context.Context, runID string, work func(context.Context) error)
}

// Handler owns the dependencies shared by request handlers.
type Handler struct {
	Store *store.Store
	// Codec is retained for backward compatibility with handlers that
	// still reach for AES-GCM directly; new code should go through
	// Secrets instead. Secret endpoints (Put/Reveal/Delete) delegate
	// to Secrets and ignore Codec.
	Codec       *crypto.Codec
	Secrets     secrets.Manager
	AppDeployer *service.AppDeployer
	WSBroadcast func(channel string, data []byte)
	// Executor runs pipeline-run goroutines invoked by RunPipeline. nil
	// is allowed in tests that do not exercise execution.
	Executor *service.Executor
	// Runs spawns tracked goroutines. nil is allowed in tests; when
	// nil, RunPipeline runs the executor synchronously inside the
	// request goroutine.
	Runs RunSpawner
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
		// Optimistic-concurrency miss: another writer moved the row's
		// version since the caller fetched it. Tell the client to
		// refetch and retry.
		c.JSON(http.StatusConflict, gin.H{
			"error": "version conflict; refetch and retry",
		})
		return true
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	return true
}
