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

// TestValidatePipelineDAG_EdgeCondition verifies the Primitive #2
// contract that replaced the T4 forward-compat refusal: every
// evaluated condition (empty, success, failure, always) is accepted;
// genuinely unknown values are still rejected because they would
// fail closed at run time.
func TestValidatePipelineDAG_EdgeCondition(t *testing.T) {
	for _, cond := range []string{"", "success", "failure", "always"} {
		t.Run("condition "+cond+" allowed", func(t *testing.T) {
			p := &model.Pipeline{
				Stages: []model.Stage{{ID: "build"}, {ID: "test"}},
				Edges:  []model.Edge{{ID: "e1", Source: "build", Target: "test", Condition: cond}},
			}
			errs := ValidatePipelineDAG(p)
			if len(errs) != 0 {
				t.Errorf("expected no errors for condition=%q, got %v", cond, errs)
			}
		})
	}

	t.Run("unknown condition rejected", func(t *testing.T) {
		p := &model.Pipeline{
			Stages: []model.Stage{{ID: "build"}, {ID: "notify"}},
			Edges:  []model.Edge{{ID: "e1", Source: "build", Target: "notify", Condition: "on-tuesdays"}},
		}
		errs := ValidatePipelineDAG(p)
		found := false
		for _, e := range errs {
			if e == `service: edge build->notify: condition "on-tuesdays" is not one of success, failure, always` {
				found = true
			}
		}
		if !found {
			t.Errorf("expected unknown condition error, got %v", errs)
		}
	})
}

// TestValidatePipelineDAG_OutputRefAncestor covers the inter-stage outputs
// reference validation (Primitive #3, dag-adaptation-2026.md §7.3 / DR-3):
// a ${stages.<id>.<key>} reference is valid only when <id> is a declared
// stage AND an ancestor of the referencing stage. Key existence is not
// validated here (runtime-only).
func TestValidatePipelineDAG_OutputRefAncestor(t *testing.T) {
	t.Run("ancestor reference allowed", func(t *testing.T) {
		p := &model.Pipeline{
			Stages: []model.Stage{
				{ID: "build", Type: model.StageTypeBuild},
				{ID: "push", Type: model.StageTypePush, Config: model.StageConfig{
					Repository: "reg/app@${stages.build.digest}",
				}},
			},
			Edges: []model.Edge{{ID: "e1", Source: "build", Target: "push"}},
		}
		if errs := ValidatePipelineDAG(p); len(errs) != 0 {
			t.Errorf("expected no errors for ancestor reference, got %v", errs)
		}
	})

	t.Run("transitive ancestor reference allowed", func(t *testing.T) {
		// build -> test -> push; push references build (a transitive ancestor).
		p := &model.Pipeline{
			Stages: []model.Stage{
				{ID: "build", Type: model.StageTypeBuild},
				{ID: "test", Type: model.StageTypeTest},
				{ID: "push", Type: model.StageTypePush, Config: model.StageConfig{
					Repository: "reg/app@${stages.build.digest}",
				}},
			},
			Edges: []model.Edge{
				{ID: "e1", Source: "build", Target: "test"},
				{ID: "e2", Source: "test", Target: "push"},
			},
		}
		if errs := ValidatePipelineDAG(p); len(errs) != 0 {
			t.Errorf("expected no errors for transitive ancestor, got %v", errs)
		}
	})

	t.Run("unknown stage reference rejected", func(t *testing.T) {
		p := &model.Pipeline{
			Stages: []model.Stage{
				{ID: "push", Type: model.StageTypePush, Config: model.StageConfig{
					Repository: "reg/app@${stages.ghost.digest}",
				}},
			},
			Edges: []model.Edge{},
		}
		errs := ValidatePipelineDAG(p)
		found := false
		for _, e := range errs {
			if e == `service: stage "push" references unknown stage "ghost" in ${stages.ghost.*}` {
				found = true
			}
		}
		if !found {
			t.Errorf("expected unknown-stage error, got %v", errs)
		}
	})

	t.Run("non-ancestor reference rejected", func(t *testing.T) {
		// build and lint are siblings (both feed deploy); push references
		// lint but has no edge path from lint, so lint's outputs can't exist
		// when push runs.
		p := &model.Pipeline{
			Stages: []model.Stage{
				{ID: "build", Type: model.StageTypeBuild},
				{ID: "lint", Type: model.StageTypeCustom},
				{ID: "push", Type: model.StageTypePush, Config: model.StageConfig{
					Repository: "reg/app@${stages.lint.digest}",
				}},
			},
			Edges: []model.Edge{
				{ID: "e1", Source: "build", Target: "push"},
				// lint is wholly disconnected from push.
			},
		}
		errs := ValidatePipelineDAG(p)
		found := false
		for _, e := range errs {
			if e == `service: stage "push" references ${stages.lint.*} but "lint" is not an ancestor (no path of edges from "lint" to "push")` {
				found = true
			}
		}
		if !found {
			t.Errorf("expected non-ancestor error, got %v", errs)
		}
	})

	t.Run("self reference rejected", func(t *testing.T) {
		p := &model.Pipeline{
			Stages: []model.Stage{
				{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{
					Context: "${stages.build.digest}",
				}},
			},
			Edges: []model.Edge{},
		}
		errs := ValidatePipelineDAG(p)
		found := false
		for _, e := range errs {
			if e == `service: stage "build" references its own outputs ${stages.build.*}` {
				found = true
			}
		}
		if !found {
			t.Errorf("expected self-reference error, got %v", errs)
		}
	})

	t.Run("reference in slice field validated", func(t *testing.T) {
		// Tags is a slice field; a non-ancestor reference there must still
		// be caught.
		p := &model.Pipeline{
			Stages: []model.Stage{
				{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{
					Tags: []string{"reg/app:${stages.ghost.tag}"},
				}},
			},
			Edges: []model.Edge{},
		}
		errs := ValidatePipelineDAG(p)
		if len(errs) == 0 {
			t.Errorf("expected error for unknown stage referenced in Tags slice, got none")
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
