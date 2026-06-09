package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/triage"
)

// TriageRunner is the slice of triage.Client the handler needs; an
// interface so tests can fake the Anthropic call.
type TriageRunner interface {
	Triage(ctx context.Context, req triage.Request) (triage.Result, error)
}

// GetCapabilities advertises optional server features to the
// frontend (authenticated — config posture isn't leaked publicly).
// Extensible map: add keys as opt-in features land.
func (h *Handler) GetCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"aiTriage": h.Triage != nil,
	})
}

// TriageStage asks the configured AI model why a failed stage failed
// (roadmap M4). Advisory only: the response is text for the operator;
// Cooker never acts on it. 503 when the feature is off; 409 when the
// stage isn't failed (triaging a green stage wastes paid tokens).
func (h *Handler) TriageStage(c *gin.Context) {
	if h.Triage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI triage not enabled on this deployment"})
		return
	}
	pipelineID := c.Param("id")
	run, ok := h.loadRunForPipeline(c, c.Param("runId"), pipelineID)
	if !ok {
		return
	}
	stageID := c.Param("stageId")
	var sr *model.StageRun
	for i := range run.StageRuns {
		if run.StageRuns[i].StageID == stageID {
			sr = &run.StageRuns[i]
			break
		}
	}
	if sr == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stage not found in run"})
		return
	}
	if sr.Status != model.RunStatusFailed {
		c.JSON(http.StatusConflict, gin.H{"error": "stage is not failed; triage applies to failed stages only"})
		return
	}

	// Stage config is cosmetic context for the prompt; a deleted
	// pipeline must not block triaging its historical run.
	var stage *model.Stage
	if p, err := h.Store.Pipelines.Get(c.Request.Context(), pipelineID); err == nil && p != nil {
		for i := range p.Stages {
			if p.Stages[i].ID == stageID {
				stage = &p.Stages[i]
				break
			}
		}
	}
	if stage == nil {
		stage = &model.Stage{ID: stageID, Name: stageID}
	}

	res, err := h.Triage.Triage(c.Request.Context(), triage.BuildRequest(stage, sr))
	if err != nil {
		switch {
		case errors.Is(err, triage.ErrRateLimited):
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "AI provider rate limit; try again shortly"})
		case errors.Is(err, triage.ErrAuth):
			c.JSON(http.StatusBadGateway, gin.H{"error": "AI provider rejected the configured API key"})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": "triage failed: " + err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"advisory": res.Advisory,
		"model":    res.Model,
	})
}
