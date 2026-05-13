package service

import (
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestValidatePipelineDAG_Valid(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build", Type: model.StageTypeBuild},
			{ID: "test", Type: model.StageTypeTest},
			{ID: "deploy", Type: model.StageTypeDeploy},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "test"},
			{ID: "e2", Source: "test", Target: "deploy"},
		},
	}

	errs := ValidatePipelineDAG(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidatePipelineDAG_Cycle(t *testing.T) {
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

	errs := ValidatePipelineDAG(p)
	if len(errs) == 0 {
		t.Fatal("expected cycle detection error")
	}
}

func TestValidatePipelineDAG_NoEdges(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build"},
			{ID: "test"},
		},
		Edges: []model.Edge{},
	}

	errs := ValidatePipelineDAG(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors for independent stages, got %v", errs)
	}
}

func TestValidatePipelineDAG_EmptyPipeline(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{},
		Edges:  []model.Edge{},
	}

	errs := ValidatePipelineDAG(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty pipeline, got %v", errs)
	}
}

// TestValidatePipelineDAG_DuplicateStageID verifies the duplicate-stage-ID
// check that was moved from handler/pipeline.go validateDAG into this
// service function per handler-layering audit Finding 1.
func TestValidatePipelineDAG_DuplicateStageID(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build"},
			{ID: "build"}, // duplicate
		},
		Edges: []model.Edge{},
	}

	errs := ValidatePipelineDAG(p)
	found := false
	for _, e := range errs {
		if e == "duplicate stage ID: build" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'duplicate stage ID: build' in errors, got %v", errs)
	}
}

// TestValidatePipelineDAG_DanglingEdge verifies the dangling-reference checks
// that were moved from handler/pipeline.go validateDAG into this service
// function per handler-layering audit Finding 1.
func TestValidatePipelineDAG_DanglingEdge(t *testing.T) {
	t.Run("unknown source", func(t *testing.T) {
		p := &model.Pipeline{
			Stages: []model.Stage{{ID: "build"}},
			Edges:  []model.Edge{{ID: "e1", Source: "missing", Target: "build"}},
		}
		errs := ValidatePipelineDAG(p)
		found := false
		for _, e := range errs {
			if e == "edge references unknown source: missing" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected unknown source error, got %v", errs)
		}
	})

	t.Run("unknown target", func(t *testing.T) {
		p := &model.Pipeline{
			Stages: []model.Stage{{ID: "build"}},
			Edges:  []model.Edge{{ID: "e1", Source: "build", Target: "missing"}},
		}
		errs := ValidatePipelineDAG(p)
		found := false
		for _, e := range errs {
			if e == "edge references unknown target: missing" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected unknown target error, got %v", errs)
		}
	})
}

// TestValidatePipelineDAG_EdgeCondition verifies the T4 forward-compat check
// (dag-adaptation-2026.md §6 T4): edges with Condition="" or Condition="success"
// are allowed; any other value is refused until Primitive #2 wires real
// evaluation in W6.
func TestValidatePipelineDAG_EdgeCondition(t *testing.T) {
	t.Run("empty condition allowed", func(t *testing.T) {
		p := &model.Pipeline{
			Stages: []model.Stage{{ID: "build"}, {ID: "test"}},
			Edges:  []model.Edge{{ID: "e1", Source: "build", Target: "test", Condition: ""}},
		}
		errs := ValidatePipelineDAG(p)
		if len(errs) != 0 {
			t.Errorf("expected no errors for empty condition, got %v", errs)
		}
	})

	t.Run("success condition allowed", func(t *testing.T) {
		p := &model.Pipeline{
			Stages: []model.Stage{{ID: "build"}, {ID: "test"}},
			Edges:  []model.Edge{{ID: "e1", Source: "build", Target: "test", Condition: "success"}},
		}
		errs := ValidatePipelineDAG(p)
		if len(errs) != 0 {
			t.Errorf("expected no errors for condition=success, got %v", errs)
		}
	})

	t.Run("failure condition rejected", func(t *testing.T) {
		p := &model.Pipeline{
			Stages: []model.Stage{{ID: "build"}, {ID: "notify"}},
			Edges:  []model.Edge{{ID: "e1", Source: "build", Target: "notify", Condition: "failure"}},
		}
		errs := ValidatePipelineDAG(p)
		found := false
		for _, e := range errs {
			if e == `service: edge build->notify: condition "failure" not yet supported (only "success" or empty)` {
				found = true
			}
		}
		if !found {
			t.Errorf("expected unsupported condition error, got %v", errs)
		}
	})
}

func TestBuildDAGFromPipeline_Valid(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build"},
			{ID: "test"},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "test"},
		},
	}

	dag, err := BuildDAGFromPipeline(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dag == nil {
		t.Fatal("expected non-nil DAG")
	}
	if len(dag.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(dag.Nodes))
	}
}

func TestBuildDAGFromPipeline_Invalid(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "a"},
			{ID: "b"},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "a"},
		},
	}

	_, err := BuildDAGFromPipeline(p)
	if err == nil {
		t.Fatal("expected error for cyclic pipeline")
	}
}

func TestBuildDAGFromPipeline_ParallelStages(t *testing.T) {
	p := &model.Pipeline{
		Stages: []model.Stage{
			{ID: "build-frontend"},
			{ID: "build-backend"},
			{ID: "integration-test"},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build-frontend", Target: "integration-test"},
			{ID: "e2", Source: "build-backend", Target: "integration-test"},
		},
	}

	dag, err := BuildDAGFromPipeline(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	levels, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("topological sort failed: %v", err)
	}

	if len(levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(levels))
	}
	if len(levels[0]) != 2 {
		t.Errorf("expected 2 parallel nodes at level 0, got %d", len(levels[0]))
	}
}
