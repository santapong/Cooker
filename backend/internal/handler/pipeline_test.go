package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

func TestValidateDAG_Valid(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build"},
			{ID: "test"},
			{ID: "deploy"},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "test"},
			{ID: "e2", Source: "test", Target: "deploy"},
		},
	}

	errs := service.ValidatePipelineDAG(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateDAG_DuplicateStageIDs(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build"},
			{ID: "build"},
		},
		Edges: []model.Edge{},
	}

	errs := service.ValidatePipelineDAG(p)
	found := false
	for _, e := range errs {
		if e == "duplicate stage ID: build" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate stage ID error, got %v", errs)
	}
}

func TestValidateDAG_UnknownEdgeSource(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build"},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "nonexistent", Target: "build"},
		},
	}

	errs := service.ValidatePipelineDAG(p)
	found := false
	for _, e := range errs {
		if e == "edge references unknown source: nonexistent" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unknown source error, got %v", errs)
	}
}

func TestValidateDAG_UnknownEdgeTarget(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build"},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "nonexistent"},
		},
	}

	errs := service.ValidatePipelineDAG(p)
	found := false
	for _, e := range errs {
		if e == "edge references unknown target: nonexistent" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unknown target error, got %v", errs)
	}
}

func TestValidateDAG_Cycle(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "a"},
			{ID: "b"},
			{ID: "c"},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "c"},
			{ID: "e3", Source: "c", Target: "a"},
		},
	}

	errs := service.ValidatePipelineDAG(p)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "cycle") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cycle error, got %v", errs)
	}
}

func TestValidateDAG_Empty(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{},
		Edges:  []model.Edge{},
	}

	errs := service.ValidatePipelineDAG(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty pipeline, got %v", errs)
	}
}

func TestValidateDAG_SingleNode(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build"},
		},
		Edges: []model.Edge{},
	}

	errs := service.ValidatePipelineDAG(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors for single node, got %v", errs)
	}
}

func TestValidateDAG_ParallelBranches(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build"},
			{ID: "lint"},
			{ID: "test-unit"},
			{ID: "test-integration"},
			{ID: "deploy"},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "lint"},
			{ID: "e2", Source: "build", Target: "test-unit"},
			{ID: "e3", Source: "build", Target: "test-integration"},
			{ID: "e4", Source: "lint", Target: "deploy"},
			{ID: "e5", Source: "test-unit", Target: "deploy"},
			{ID: "e6", Source: "test-integration", Target: "deploy"},
		},
	}

	errs := service.ValidatePipelineDAG(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors for diamond DAG, got %v", errs)
	}
}

// TestGetStageLogs_ReturnsLogsAndHandlesUnknownStage exercises the
// real read added in B1 (per-stage WS log channel work). The
// historical handler returned {"logs": ""} unconditionally; this
// test pins the new behaviour: scoped to (pipelineId, runId, stageId)
// with 404 fallback when the stage does not exist on the run.
func TestGetStageLogs_ReturnsLogsAndHandlesUnknownStage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler(t)
	ctx := context.Background()

	if err := h.Store.Pipelines.Create(ctx, &model.Pipeline{ID: "pipe", Name: "pipe"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Runs.Create(ctx, &model.PipelineRun{
		ID:         "run-1",
		PipelineID: "pipe",
		StageRuns: []model.StageRun{
			{StageID: "build", Logs: "line one\nline two\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.GET("/pipelines/:id/runs/:runId/logs/:stageId", h.GetStageLogs)

	t.Run("happy path", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pipelines/pipe/runs/run-1/logs/build", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
		}
		var payload map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v body=%s", err, w.Body.String())
		}
		if payload["logs"] != "line one\nline two\n" {
			t.Errorf("logs payload: got %q", payload["logs"])
		}
	})

	t.Run("unknown stage 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pipelines/pipe/runs/run-1/logs/missing", nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for unknown stage, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("cross-pipeline 404", func(t *testing.T) {
		// Defence-in-depth: the IDOR guard from T3 must keep firing on
		// the new code path. Run a sibling pipeline and try to read its
		// run via this pipeline; should be 404, not 200.
		if err := h.Store.Pipelines.Create(ctx, &model.Pipeline{ID: "pipe2", Name: "pipe2"}); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pipelines/pipe2/runs/run-1/logs/build", nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 (cross-pipeline access denied), got %d", w.Code)
		}
	})
}
