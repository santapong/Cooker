package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/validate"
)

// validatePipelineInput rejects malformed pipeline payloads. Called
// from CreatePipeline and UpdatePipeline before any store write.
func validatePipelineInput(p *model.Pipeline) error {
	if err := validate.Name("name", p.Name); err != nil {
		return err
	}
	if err := validate.Description("description", p.Description); err != nil {
		return err
	}
	for i, s := range p.Stages {
		if err := validate.StageType(s.Type); err != nil {
			return err
		}
		_ = i
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

	// Validate DAG structure at the service layer. Replaces the private
	// validateDAG that was deleted per handler-layering audit Finding 1.
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

	// Delegate to service.ValidatePipelineDAG (which calls dagrunner.DAG.Validate)
	// instead of the private validateDAG that was deleted from this file.
	// See handler-layering audit Finding 1.
	dagErrs := service.ValidatePipelineDAG(p)
	if len(dagErrs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "errors": dagErrs})
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": true, "errors": []string{}})
}

// RunPipeline kicks off a new run for the pipeline. Rate-limited.
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

	// Spawn the executor in a tracked goroutine. The handler returns
	// 202 immediately with the pending run; the caller polls / streams
	// status. Without an Executor or RunSpawner the run stays pending
	// (matches the old behaviour for tests that don't wire either).
	//
	// F2 (docs/audits/2026-05-handler-layering.md Finding 2): the
	// terminal-status state machine lives inside Executor.Execute now;
	// the handler's job is to persist the run as Execute returned it.
	// Do NOT re-derive run.Status from runCopy.Status here — Execute
	// guarantees a terminal value and Cancelled stays Cancelled.
	if h.Runs != nil && h.Executor != nil {
		// Use a fresh background context so the run survives the
		// completion of the HTTP request that started it.
		h.Runs.Spawn(context.Background(), run.ID, func(ctx context.Context) error {
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

