package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

// runCanaryStartCtx is the background worker for a canary deploy. It
// mirrors runAppDeployCtx (a coordinator-owned ctx, a WS log sink, and a
// terminal run-row update) but drives CanaryService.Start, which builds
// the new image and establishes the weighted split.
func (h *Handler) runCanaryStartCtx(ctx context.Context, a *model.App, runID, channel string) {
	deployCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	sink := &wsLogSink{channel: channel, broadcast: h.WSBroadcast}
	sink.writef("[start] canary app=%s repo=%s branch=%s run=%s\n", a.Name, a.GitHubRepo, a.Branch, runID)

	c, err := h.Canary.Start(deployCtx, a, runID, sink)
	status := model.RunStatusFailed
	if err != nil {
		sink.writef("[error] %v\n", err)
	} else {
		status = model.RunStatusSuccess
		sink.writef("[canary] %s at %d%% (auto-promote=%t)\n", c.Status, c.Weight, c.AutoPromote)
	}
	// Update the stub run row created before Spawn so the run list reflects
	// the terminal state (mirrors runAppDeployCtx). The canary's own
	// lifecycle lives on the app_canaries row, not the run row.
	if h.Runs != nil && h.Store != nil {
		now := time.Now()
		run := &model.PipelineRun{
			ID:         runID,
			PipelineID: a.ID,
			Status:     status,
			FinishedAt: &now,
		}
		if updateErr := h.Store.Runs.Update(deployCtx, run); updateErr != nil {
			sink.writef("[warn] persist run: %v\n", updateErr)
		}
	}
	sink.writef("[end] run=%s\n", runID)
}

// PromoteAppCanary shifts all traffic to the canary version and resolves
// the canary as promoted. Route: POST /apps/:id/canary/promote.
func (h *Handler) PromoteAppCanary(c *gin.Context) {
	if h.Canary == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "canary not available"})
		return
	}
	canary, err := h.Canary.Promote(c.Request.Context(), c.Param("id"))
	if abortCanaryErr(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"canary": canary})
}

// AbortAppCanary rolls traffic back to the stable version and resolves
// the canary as aborted. Route: POST /apps/:id/canary/abort.
func (h *Handler) AbortAppCanary(c *gin.Context) {
	if h.Canary == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "canary not available"})
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	// Body is optional (a bare abort is fine); only parse when present.
	if c.Request.ContentLength > 0 {
		if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": bindErr.Error()})
			return
		}
	}
	canary, err := h.Canary.Abort(c.Request.Context(), c.Param("id"), req.Reason)
	if abortCanaryErr(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"canary": canary})
}

// GetAppCanary returns the app's active canary, or 404 when none is in
// flight. Route: GET /apps/:id/canary. The status is also embedded in
// GetApp's response so the detail page gets it without a second call,
// but this dedicated endpoint backs the canary panel's polling.
func (h *Handler) GetAppCanary(c *gin.Context) {
	if h.Canary == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "canary not available"})
		return
	}
	canary, err := h.Canary.Status(c.Request.Context(), c.Param("id"))
	if abortCanaryErr(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"canary": canary})
}

// abortCanaryErr maps CanaryService errors to HTTP responses. Returns
// true when a response was written (caller should return).
func abortCanaryErr(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, service.ErrNoActiveCanary):
		c.JSON(http.StatusNotFound, gin.H{"error": "no active canary"})
	case errors.Is(err, service.ErrCanaryUnsupported):
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "canary deployments require a Kubernetes deploy target with a weighted-capable deployer",
		})
	case errors.Is(err, service.ErrCanaryInFlight):
		c.JSON(http.StatusConflict, gin.H{"error": "a canary is already in flight for this app"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
	return true
}
