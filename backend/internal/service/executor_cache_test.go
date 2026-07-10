package service

import (
	"context"
	"testing"

	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/model"
)

// TestExecutor_BuildStage_MapsCacheSpec pins the model→builder cache
// mapping: a stage with a CacheSpec hands the builder the mirrored
// values; a stage without one hands the zero value (cache off).
func TestExecutor_BuildStage_MapsCacheSpec(t *testing.T) {
	mb := &mockBuilder{res: builder.Result{ImageID: "sha256:1", Tags: []string{"t"}}}

	p := &model.Pipeline{
		ID: "pipe-cache",
		Stages: []model.Stage{
			{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Context: "/ctx", Tags: []string{"t"},
				Cache: &model.CacheSpec{Mode: "registry", Ref: "reg.example.com/app/cache", Inline: true},
			}},
		},
	}
	run := &model.PipelineRun{
		ID: "run-cache", PipelineID: "pipe-cache", Status: model.RunStatusPending,
		StageRuns: []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithBuilder(mb))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := mb.calls[0]
	if req.Cache.Mode != "registry" || req.Cache.Ref != "reg.example.com/app/cache" || !req.Cache.Inline {
		t.Errorf("cache spec not mapped: got %+v", req.Cache)
	}
}

func TestExecutor_BuildStage_NilCacheMapsToZero(t *testing.T) {
	mb := &mockBuilder{res: builder.Result{Tags: []string{"t"}}}
	p := &model.Pipeline{
		ID: "pipe-nocache",
		Stages: []model.Stage{
			{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Context: "/ctx", Tags: []string{"t"},
			}},
		},
	}
	run := &model.PipelineRun{
		ID: "run-nocache", PipelineID: "pipe-nocache", Status: model.RunStatusPending,
		StageRuns: []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}
	exec := NewExecutor(WithBuilder(mb))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mb.calls[0].Cache; got != (builder.CacheSpec{}) {
		t.Errorf("expected zero CacheSpec for nil stage cache, got %+v", got)
	}
}
