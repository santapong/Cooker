package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
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
		// Snapshot the run BEFORE spawning. Once SpawnWithDeadline starts
		// the executor goroutine it mutates `run` (and its StageRuns) in
		// place, so encoding `run` directly in the 202 response would race
		// the executor (F15, docs/proposals/run-state-concurrency-2026.md).
		// Taken here on this goroutine, the snapshot is sequenced-before
		// the spawn.
		snapshot := run.Clone()
		// Per-pipeline RunDeadline override; 0 falls back to the
		// cluster default inside the coordinator.
		h.Runs.SpawnWithDeadline(context.Background(), run.ID, service.PipelineRunDeadline(p), func(ctx context.Context) error {
			_, execErr := h.Executor.Execute(ctx, p, run)
			if err := h.Store.Runs.Update(ctx, run); err != nil {
				return err
			}
			// Emit the terminal-run event. The inline path owns this;
			// the queue path emits from JobQueueRunner instead. A run
			// takes exactly one path, so this never double-sends.
			service.NotifyRunOutcome(h.Dispatcher, p, run, execErr)
			return execErr
		})
		c.JSON(http.StatusAccepted, snapshot)
		return
	}
	// No executor/spawner wired: the run stays pending and is not mutated,
	// so encoding it directly is safe.
	c.JSON(http.StatusAccepted, run)
}

// Run-history pagination bounds. The default keeps the common "recent
// activity" view to one page; the max stops a single request from
// dragging the whole history (runs carry three JSONB blobs each).
const (
	listRunsDefaultLimit = 50
	listRunsMaxLimit     = 200
)

// intQuery parses an integer query param, falling back to def when the
// param is absent or malformed (cosmetic params don't 400, matching
// the tailLines convention) and clamping the result to [min, max].
func intQuery(c *gin.Context, name string, def, min, max int) int {
	out := def
	if v := c.Query(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			out = n
		}
	}
	if out < min {
		out = min
	}
	if out > max {
		out = max
	}
	return out
}

func (h *Handler) ListPipelineRuns(c *gin.Context) {
	limit := intQuery(c, "limit", listRunsDefaultLimit, 1, listRunsMaxLimit)
	offset := intQuery(c, "offset", 0, 0, 1<<30)
	runs, err := h.Store.Runs.List(c.Request.Context(), c.Param("id"), limit, offset)
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
	// Status-only write: UpdateStatus touches just status/finished_at/error,
	// so it neither re-marshals the run's JSONB blobs (F18) nor overwrites
	// stage_runs / heartbeat_at with this handler's stale loaded copy.
	if err := h.Store.Runs.UpdateStatus(c.Request.Context(), run.ID, model.RunStatusCancelled, run.FinishedAt, run.Error); err != nil {
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
