package handler

import (
	"testing"

	"github.com/cooker-ci/cooker/internal/model"
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

	errs := validateDAG(p)
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

	errs := validateDAG(p)
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

	errs := validateDAG(p)
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

	errs := validateDAG(p)
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

	errs := validateDAG(p)
	found := false
	for _, e := range errs {
		if e == "pipeline contains a cycle" {
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

	errs := validateDAG(p)
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

	errs := validateDAG(p)
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

	errs := validateDAG(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors for diamond DAG, got %v", errs)
	}
}
