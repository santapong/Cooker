package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
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
		if err := validate.CacheSpec(s.Config.Cache); err != nil {
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
	h.createPipeline(c, &p)
}

// createPipeline runs the shared create path: per-field validation,
// DAG validation, server-field assignment (fresh ID + timestamps),
// nil-slice normalisation, and the store write. CreatePipeline (JSON
// body) and ImportPipeline (YAML body) both funnel through here so an
// imported pipeline is validated and persisted byte-for-byte the same
// way the editor's create is — one source of truth for the error
// envelope and the server-assigned fields. The caller is responsible
// for having populated p's authored fields; this overwrites any
// client-supplied ID/timestamps/version.
func (h *Handler) createPipeline(c *gin.Context, p *model.Pipeline) {
	if err := validatePipelineInput(p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if dagErrs := service.ValidatePipelineDAG(p); len(dagErrs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "errors": dagErrs})
		return
	}

	p.ID = uuid.New().String()
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	// Server owns the optimistic-concurrency token; a YAML/JSON body
	// must not be able to seed it.
	p.Version = 0

	if p.Variables == nil {
		p.Variables = make(map[string]string)
	}
	if p.Stages == nil {
		p.Stages = []model.Stage{}
	}
	if p.Edges == nil {
		p.Edges = []model.Edge{}
	}

	if err := h.Store.Pipelines.Create(c.Request.Context(), p); err != nil {
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

// maxImportYAMLBytes caps the import body. A pipeline document is a
// handful of KB even with many stages; 1 MiB is generous headroom while
// stopping a single request from buffering an unbounded body into
// memory before the YAML parser sees it.
const maxImportYAMLBytes = 1 << 20

// pipelineFilenameRe strips a pipeline name down to a filesystem-safe
// slug for the suggested download filename. Anything outside the class
// collapses to a single hyphen.
var pipelineFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// pipelineYAMLFilename derives "<slug>.cooker.yaml" from a pipeline
// name, falling back to "pipeline" when the slug is empty. The
// .cooker.yaml suffix signals the document is a Cooker pipeline-as-code
// file without claiming the bare .yaml association.
func pipelineYAMLFilename(name string) string {
	slug := strings.Trim(pipelineFilenameRe.ReplaceAllString(name, "-"), "-.")
	if slug == "" {
		slug = "pipeline"
	}
	const maxSlug = 80
	if len(slug) > maxSlug {
		slug = strings.Trim(slug[:maxSlug], "-.")
	}
	return slug + ".cooker.yaml"
}

// ExportPipeline renders a pipeline as a pipeline-as-code YAML document
// (product-plan Tier 2). Read-level RBAC: same gate as GET /pipelines/:id.
// Server-assigned fields (id, timestamps, version) are stripped by the
// service so the document is portable and version-controllable.
//
// @Summary      Export a pipeline as YAML
// @Tags         pipelines
// @Produce      application/yaml
// @Param        id   path      string  true  "Pipeline ID"
// @Success      200  {string}  string  "Pipeline-as-code YAML document"
// @Failure      404  {object}  map[string]string
// @Security     BearerAuth
// @Router       /pipelines/{id}/export [get]
func (h *Handler) ExportPipeline(c *gin.Context) {
	p, err := h.Store.Pipelines.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}
	out, err := service.ExportPipelineYAML(p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", pipelineYAMLFilename(p.Name)))
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", out)
}

// ImportPipeline creates a NEW pipeline from a pipeline-as-code YAML
// document (product-plan Tier 2). Same write gate as CreatePipeline.
//
// The body is the raw YAML document (Content-Type application/yaml or
// text/yaml). The envelope's apiVersion/kind are checked first; an
// unknown value, malformed YAML, or an empty body all 400. The parsed
// pipeline then runs the EXACT create path (validatePipelineInput +
// ValidatePipelineDAG + fresh ID/timestamps) so an invalid import fails
// with the same error envelope the editor sees, and a fresh ID is
// always minted — importing the same file twice yields two pipelines
// (no name-collision handling; create has none and the model has no
// unique-name constraint).
//
// @Summary      Import a pipeline from YAML
// @Tags         pipelines
// @Accept       application/yaml
// @Produce      json
// @Success      201  {object}  model.Pipeline
// @Failure      400  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Security     BearerAuth
// @Router       /pipelines/import [post]
func (h *Handler) ImportPipeline(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxImportYAMLBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not read request body"})
		return
	}
	if len(body) > maxImportYAMLBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "pipeline document too large"})
		return
	}

	p, err := service.ImportPipelineYAML(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.createPipeline(c, p)
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
	// BC-H2: use the server-fetched version for the OCC WHERE clause so
	// the client cannot bypass the stale-check by supplying an arbitrary
	// version field. ErrConflict → 409 is handled by abortStoreErr.
	p.Version = existing.Version

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

// GetRunDiff compares a run against a baseline run of the same
// pipeline (roadmap M3). ?against=<runId> picks an explicit baseline;
// the default "last-success" resolves to the most recent successful
// run created before this one.
func (h *Handler) GetRunDiff(c *gin.Context) {
	pipelineID := c.Param("id")
	base, ok := h.loadRunForPipeline(c, c.Param("runId"), pipelineID)
	if !ok {
		return
	}
	// Stage names are cosmetic on the diff; a missing pipeline row
	// (e.g. deleted since the run) must not 404 the comparison.
	p, _ := h.Store.Pipelines.Get(c.Request.Context(), pipelineID)

	againstParam := c.Query("against")
	var against *model.PipelineRun
	switch {
	case againstParam == "" || againstParam == "last-success":
		// Bounded at 200: looking further back than this to find a
		// successful baseline is a reasonable cap; if no success exists
		// in the last 200 runs we report "no successful prior run" below.
		// The list path strips logs in the store, so rows are cheap.
		const runDiffSearchLimit = 200
		runs, err := h.Store.Runs.List(c.Request.Context(), pipelineID, runDiffSearchLimit, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, r := range runs {
			if r.ID != base.ID && r.Status == model.RunStatusSuccess && r.CreatedAt.Before(base.CreatedAt) {
				against = r
				break
			}
		}
		if against == nil {
			c.JSON(http.StatusOK, service.RunDiff{Reason: "no successful prior run", Stages: []service.StageDiff{}})
			return
		}
	default:
		var okAgainst bool
		against, okAgainst = h.loadRunForPipeline(c, againstParam, pipelineID)
		if !okAgainst {
			return
		}
	}
	c.JSON(http.StatusOK, service.BuildRunDiff(base, against, p))
}

// GetPipelineAnalytics computes stage-duration and success-rate
// statistics from recent run history (roadmap M4). ?runs=N bounds the
// sample (default 30, max 200).
func (h *Handler) GetPipelineAnalytics(c *gin.Context) {
	pipelineID := c.Param("id")
	p, err := h.Store.Pipelines.Get(c.Request.Context(), pipelineID)
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}
	n := 30
	if v := c.Query("runs"); v != "" {
		if parsed, perr := strconv.Atoi(v); perr == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > 200 {
		n = 200
	}
	// List is newest-first and bounds the window in SQL (logs are
	// stripped on the list path — analytics only reads timestamps,
	// statuses and outputs).
	runs, err := h.Store.Runs.List(c.Request.Context(), pipelineID, n, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, service.ComputeAnalytics(p, runs))
}
