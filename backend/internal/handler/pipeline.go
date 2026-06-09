package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/validate"
)

// bearerFromAuthHeader returns the raw bearer token (no "Bearer " prefix).
// Empty string + false when the header is absent or malformed. Used by
// RunPipeline to compute a forensic hash; the token itself is not retained.
func bearerFromAuthHeader(h string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	return tok, tok != ""
}

// validatePipelineInput rejects malformed pipeline payloads. Called
// from CreatePipeline and UpdatePipeline before any store write.
func validatePipelineInput(p *model.Pipeline) error {
	if err := validate.Name("name", p.Name); err != nil {
		return err
	}
	if err := validate.Description("description", p.Description); err != nil {
		return err
	}
	if err := validate.RunDeadline(p.RunDeadline); err != nil {
		return err
	}
	for i, s := range p.Stages {
		if err := validate.StageType(s.Type); err != nil {
			return err
		}
		if err := validate.RetryPolicy(s.Config.Retry); err != nil {
			return fmt.Errorf("stage %d: %w", i, err)
		}
	}
	return nil
}

// ListPipelines returns all pipelines visible to the caller.
//
// @Summary      List pipelines
// @Tags         pipelines
// @Produce      json
// @Success      200  {array}   model.Pipeline
// @Failure      401  {object}  map[string]string
// @Security     BearerAuth
// @Router       /pipelines [get]
func (h *Handler) ListPipelines(c *gin.Context) {
	pipelines, err := h.Store.Pipelines.List(c.Request.Context())
	if abortStoreErr(c, err, "pipelines not found") {
		return
	}
	if pipelines == nil {
		pipelines = []*model.Pipeline{}
	}
	c.JSON(http.StatusOK, pipelines)
}

func (h *Handler) CreatePipeline(c *gin.Context) {
	var p model.Pipeline
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validatePipelineInput(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if dagErrs := service.ValidatePipelineDAG(&p); len(dagErrs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "errors": dagErrs})
		return
	}

	p.ID = uuid.New().String()
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now

	if p.Variables == nil {
		p.Variables = make(map[string]string)
	}
	if p.Stages == nil {
		p.Stages = []model.Stage{}
	}
	if p.Edges == nil {
		p.Edges = []model.Edge{}
	}

	if err := h.Store.Pipelines.Create(c.Request.Context(), &p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (h *Handler) GetPipeline(c *gin.Context) {
	p, err := h.Store.Pipelines.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) UpdatePipeline(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.Store.Pipelines.Get(c.Request.Context(), id)
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}

	var p model.Pipeline
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validatePipelineInput(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p.ID = id
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now()

	if err := h.Store.Pipelines.Update(c.Request.Context(), &p); err != nil {
		if abortStoreErr(c, err, "pipeline not found") {
			return
		}
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) DeletePipeline(c *gin.Context) {
	if err := h.Store.Pipelines.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "pipeline not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *Handler) ValidatePipeline(c *gin.Context) {
	p, err := h.Store.Pipelines.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}
	dagErrs := service.ValidatePipelineDAG(p)
	if len(dagErrs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "errors": dagErrs})
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": true, "errors": []string{}})
}

// RunPipeline kicks off a new run for the pipeline. Rate-limited.
//
// When COOKER_JOBQUEUE_ENABLED=true and the server wired an Enqueuer,
// this returns 202 immediately after persisting the pending run and
// enqueueing a job; a worker (service.JobQueueRunner) picks it up.
// Otherwise the existing inline Runs.Spawn path runs unchanged.
//
// @Summary      Run a pipeline
// @Tags         pipelines
// @Param        id    path      string  true  "Pipeline ID"
// @Produce      json
// @Success      202   {object}  model.PipelineRun
// @Failure      400   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      429   {object}  map[string]string  "Rate limit exceeded"
// @Security     BearerAuth
// @Router       /pipelines/{id}/run [post]
func (h *Handler) RunPipeline(c *gin.Context) {
	p, err := h.Store.Pipelines.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}

	run := &model.PipelineRun{
		ID:         uuid.New().String(),
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns:  make([]model.StageRun, 0, len(p.Stages)),
		Variables:  p.Variables,
		// Definition stamp for run-diff: which pipeline version this
		// run executed. 0 only on rows predating migration 017.
		PipelineVersion: p.Version,
	}

	// Capture the actor that started the run so the deploy-stage executor
	// can consult the governance gate on their behalf later, when their
	// bearer is no longer in hand. Token hash is for audit forensics; the
	// raw token is never persisted. Dev mode (no OIDC) yields an empty
	// Subject which leaves the columns blank — the executor treats that as
	// "skip governance" (pre-Phase-4 path).
	if u := auth.GetUser(c); u != nil {
		run.StartedByUserSub = u.Subject
		run.StartedByEmail = u.Email
		run.StartedByGroups = append([]string(nil), u.Groups...)
	}
	if raw, ok := bearerFromAuthHeader(c.GetHeader("Authorization")); ok {
		sum := sha256.Sum256([]byte(raw))
		run.StartedByTokenHash = hex.EncodeToString(sum[:16])
	}

	for _, stage := range p.Stages {
		run.StageRuns = append(run.StageRuns, model.StageRun{
			StageID: stage.ID,
			Status:  model.RunStatusPending,
		})
	}

	if err := h.Store.Runs.Create(c.Request.Context(), run); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Durable path (Phase-1 / A1): enqueue and return 202. The worker
	// pool picks up the job via Dequeue + NOTIFY and runs the
	// executor via service.JobQueueRunner. Enabled by setting
	// COOKER_JOBQUEUE_ENABLED=true at boot.
	if h.Enqueuer != nil {
		if err := h.Enqueuer.EnqueueRun(c.Request.Context(), p.ID, run.ID); err != nil {
			// Don't fail the request: the run row exists and the
			// orphan sweep will recover it on the next boot if no
			// worker ever picks it up. The handler returns 202 so
			// the client polls / streams status and observes the
			// outcome via the existing run-detail endpoint.
			slog.Warn("RunPipeline: enqueue failed",
				"run", run.ID, "pipeline", p.ID, "err", err)
		}
		c.JSON(http.StatusAccepted, run)
		return
	}

	// Inline path (pre-Phase-1 behaviour, still the default). Spawn the
	// executor in a tracked goroutine. The handler returns 202
	// immediately with the pending run; the caller polls / streams
	// status. Without an Executor or RunSpawner the run stays pending
	// (matches the old behaviour for tests that don't wire either).
	//
	// F2 (docs/audits/2026-05-handler-layering.md Finding 2): the
	// terminal-status state machine lives inside Executor.Execute now;
	// the handler's job is to persist the run as Execute returned it.
	// Do NOT re-derive run.Status from runCopy.Status here — Execute
	// guarantees a terminal value and Cancelled stays Cancelled.
	if h.Runs != nil && h.Executor != nil {
		// Per-pipeline RunDeadline override; 0 falls back to the
		// cluster default inside the coordinator.
		h.Runs.SpawnWithDeadline(context.Background(), run.ID, service.PipelineRunDeadline(p), func(ctx context.Context) error {
			runCopy := run
			_, execErr := h.Executor.Execute(ctx, p, runCopy)
			if err := h.Store.Runs.Update(ctx, runCopy); err != nil {
				return err
			}
			return execErr
		})
	}
	c.JSON(http.StatusAccepted, run)
}

func (h *Handler) ListPipelineRuns(c *gin.Context) {
	runs, err := h.Store.Runs.List(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "runs not found") {
		return
	}
	if runs == nil {
		runs = []*model.PipelineRun{}
	}
	c.JSON(http.StatusOK, runs)
}

func (h *Handler) GetPipelineRun(c *gin.Context) {
	run, ok := h.loadRunForPipeline(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	c.JSON(http.StatusOK, run)
}

func (h *Handler) CancelPipelineRun(c *gin.Context) {
	run, ok := h.loadRunForPipeline(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	run.Status = model.RunStatusCancelled
	if err := h.Store.Runs.Update(c.Request.Context(), run); err != nil {
		if abortStoreErr(c, err, "run not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "cancelled", "runId": run.ID})
}

// GetStageLogs returns the on-disk log capture for a single stage of a
// pipeline run. The frontend uses this for backfill on first paint;
// live tail comes from the per-stage WebSocket channel exposed by
// /ws/runs/:runId/stages/:stageId/logs (see internal/server/router.go).
func (h *Handler) GetStageLogs(c *gin.Context) {
	run, ok := h.loadRunForPipeline(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	stageID := c.Param("stageId")
	for i := range run.StageRuns {
		if run.StageRuns[i].StageID == stageID {
			c.JSON(http.StatusOK, gin.H{"logs": run.StageRuns[i].Logs})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "stage not found"})
}
