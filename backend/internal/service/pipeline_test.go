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
