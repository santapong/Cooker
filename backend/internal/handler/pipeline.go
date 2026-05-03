package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cooker-ci/cooker/internal/model"
)

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

	errors := validateDAG(p)
	if len(errors) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "errors": errors})
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
	if h.Runs != nil && h.Executor != nil {
		// Use a fresh background context so the run survives the
		// completion of the HTTP request that started it.
		h.Runs.Spawn(context.Background(), run.ID, func(ctx context.Context) error {
			runCopy := run
			execErr := h.Executor.Execute(ctx, p, runCopy)
			finished := time.Now()
			runCopy.FinishedAt = &finished
			if execErr != nil {
				if runCopy.Status != model.RunStatusFailed {
					runCopy.Status = model.RunStatusFailed
				}
				if runCopy.Error == "" {
					runCopy.Error = execErr.Error()
				}
			} else if runCopy.Status == model.RunStatusRunning {
				runCopy.Status = model.RunStatusSuccess
			}
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
	run, err := h.Store.Runs.Get(c.Request.Context(), c.Param("runId"))
	if abortStoreErr(c, err, "run not found") {
		return
	}
	c.JSON(http.StatusOK, run)
}

func (h *Handler) CancelPipelineRun(c *gin.Context) {
	run, err := h.Store.Runs.Get(c.Request.Context(), c.Param("runId"))
	if abortStoreErr(c, err, "run not found") {
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

func (h *Handler) GetStageLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"logs": ""})
}

// validateDAG checks for cycles and unresolved references.
func validateDAG(p *model.Pipeline) []string {
	var errors []string

	stageIDs := make(map[string]bool)
	for _, s := range p.Stages {
		if stageIDs[s.ID] {
			errors = append(errors, "duplicate stage ID: "+s.ID)
		}
		stageIDs[s.ID] = true
	}

	for _, e := range p.Edges {
		if !stageIDs[e.Source] {
			errors = append(errors, "edge references unknown source: "+e.Source)
		}
		if !stageIDs[e.Target] {
			errors = append(errors, "edge references unknown target: "+e.Target)
		}
	}

	// Cycle detection using Kahn's algorithm
	inDegree := make(map[string]int)
	adj := make(map[string][]string)
	for _, s := range p.Stages {
		inDegree[s.ID] = 0
	}
	for _, e := range p.Edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		inDegree[e.Target]++
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adj[node] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if visited != len(p.Stages) {
		errors = append(errors, "pipeline contains a cycle")
	}

	return errors
}
