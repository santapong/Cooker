package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

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
