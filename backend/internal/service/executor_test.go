package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cooker-ci/cooker/internal/builder"
	"github.com/cooker-ci/cooker/internal/model"
)

// mockBuilder records every Build call and returns a configured
// response. Safe for concurrent use.
type mockBuilder struct {
	mu    sync.Mutex
	calls []builder.Request
	res   builder.Result
	err   error
}

func (m *mockBuilder) Build(_ context.Context, req builder.Request) (builder.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.err != nil {
		return builder.Result{}, m.err
	}
	return m.res, nil
}

func (m *mockBuilder) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func TestExecutor_ExecuteSimplePipeline(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-1",
		Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Dockerfile: "Dockerfile",
				Context:    ".",
				Tags:       []string{"latest"},
			}},
			{ID: "test", Name: "Test", Type: model.StageTypeTest, Config: model.StageConfig{
				Image:   "app:latest",
				Command: []string{"npm", "test"},
			}},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "test"},
		},
	}

	run := &model.PipelineRun{
		ID:         "run-1",
		PipelineID: "pipe-1",
		Status:     model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusPending},
			{StageID: "test", Status: model.RunStatusPending},
		},
	}

	executor := NewExecutor()
	err := executor.Execute(context.Background(), p, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if run.Status != model.RunStatusSuccess {
		t.Errorf("expected run status success, got %s", run.Status)
	}
	if run.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
	if run.FinishedAt == nil {
		t.Error("expected FinishedAt to be set")
	}

	for _, sr := range run.StageRuns {
		if sr.Status != model.RunStatusSuccess {
			t.Errorf("expected stage %s status success, got %s", sr.StageID, sr.Status)
		}
		if sr.StartedAt == nil {
			t.Errorf("expected stage %s StartedAt to be set", sr.StageID)
		}
		if sr.FinishedAt == nil {
			t.Errorf("expected stage %s FinishedAt to be set", sr.StageID)
		}
	}
}

func TestExecutor_AllStageTypes(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-all",
		Stages: []model.Stage{
			{ID: "s1", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{}},
			{ID: "s2", Name: "Test", Type: model.StageTypeTest, Config: model.StageConfig{}},
			{ID: "s3", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{}},
			{ID: "s4", Name: "Deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{}},
			{ID: "s5", Name: "Approval", Type: model.StageTypeApproval, Config: model.StageConfig{}},
			{ID: "s6", Name: "Custom", Type: model.StageTypeCustom, Config: model.StageConfig{}},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "s1", Target: "s2"},
			{ID: "e2", Source: "s2", Target: "s3"},
			{ID: "e3", Source: "s3", Target: "s4"},
			{ID: "e4", Source: "s4", Target: "s5"},
			{ID: "e5", Source: "s5", Target: "s6"},
		},
	}

	stageRuns := make([]model.StageRun, len(p.Stages))
	for i, s := range p.Stages {
		stageRuns[i] = model.StageRun{StageID: s.ID, Status: model.RunStatusPending}
	}

	run := &model.PipelineRun{
		ID:         "run-all",
		PipelineID: "pipe-all",
		Status:     model.RunStatusPending,
		StageRuns:  stageRuns,
	}

	executor := NewExecutor()
	err := executor.Execute(context.Background(), p, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if run.Status != model.RunStatusSuccess {
		t.Errorf("expected success, got %s", run.Status)
	}
}

func TestExecutor_UnknownStageType(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-unknown",
		Stages: []model.Stage{
			{ID: "s1", Name: "Unknown", Type: model.StageType("unknown"), Config: model.StageConfig{}},
		},
		Edges: []model.Edge{},
	}

	run := &model.PipelineRun{
		ID:         "run-unknown",
		PipelineID: "pipe-unknown",
		Status:     model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "s1", Status: model.RunStatusPending},
		},
	}

	executor := NewExecutor()
	err := executor.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("expected error for unknown stage type")
	}

	if run.Status != model.RunStatusFailed {
		t.Errorf("expected failed status, got %s", run.Status)
	}
}

func TestExecutor_InvalidDAG(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-cycle",
		Stages: []model.Stage{
			{ID: "a"},
			{ID: "b"},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "a"},
		},
	}

	run := &model.PipelineRun{
		ID:         "run-cycle",
		PipelineID: "pipe-cycle",
		Status:     model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "a", Status: model.RunStatusPending},
			{StageID: "b", Status: model.RunStatusPending},
		},
	}

	executor := NewExecutor()
	err := executor.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("expected error for cyclic DAG")
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-cancel",
		Stages: []model.Stage{
			{ID: "s1", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{}},
			{ID: "s2", Name: "Test", Type: model.StageTypeTest, Config: model.StageConfig{}},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "s1", Target: "s2"},
		},
	}

	run := &model.PipelineRun{
		ID:         "run-cancel",
		PipelineID: "pipe-cancel",
		Status:     model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "s1", Status: model.RunStatusPending},
			{StageID: "s2", Status: model.RunStatusPending},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	executor := NewExecutor()
	err := executor.Execute(ctx, p, run)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestExecutor_BuildStage_DispatchesToBuilder(t *testing.T) {
	mb := &mockBuilder{res: builder.Result{
		ImageID: "sha256:deadbeef",
		Tags:    []string{"registry.example.com/app:v1"},
	}}

	p := &model.Pipeline{
		ID: "pipe-build",
		Stages: []model.Stage{
			{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Dockerfile: "Dockerfile",
				Context:    "/tmp/ctx",
				Tags:       []string{"registry.example.com/app:v1"},
				BuildArgs:  map[string]string{"GO_VERSION": "1.24"},
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-build",
		PipelineID: "pipe-build",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithBuilder(mb))
	if err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mb.callCount() != 1 {
		t.Fatalf("expected 1 build call, got %d", mb.callCount())
	}
	req := mb.calls[0]
	if req.ContextDir != "/tmp/ctx" {
		t.Errorf("ContextDir: got %q", req.ContextDir)
	}
	if req.Dockerfile != "Dockerfile" {
		t.Errorf("Dockerfile: got %q", req.Dockerfile)
	}
	if len(req.Tags) != 1 || req.Tags[0] != "registry.example.com/app:v1" {
		t.Errorf("Tags: got %v", req.Tags)
	}
	if req.BuildArgs["GO_VERSION"] != "1.24" {
		t.Errorf("BuildArgs missing: got %v", req.BuildArgs)
	}

	sr := run.StageRuns[0]
	if sr.Status != model.RunStatusSuccess {
		t.Errorf("stage status: got %s", sr.Status)
	}
	if len(sr.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(sr.Artifacts))
	}
	art := sr.Artifacts[0]
	if art.Type != "oci-image" || art.Ref != "registry.example.com/app:v1" || art.Digest != "sha256:deadbeef" {
		t.Errorf("artifact: got %+v", art)
	}
}

func TestExecutor_BuildStage_BuilderErrorFailsStage(t *testing.T) {
	wantErr := errors.New("boom")
	mb := &mockBuilder{err: wantErr}

	p := &model.Pipeline{
		ID: "pipe-buildfail",
		Stages: []model.Stage{
			{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Context: "/tmp/ctx",
				Tags:    []string{"app:v1"},
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-buildfail",
		PipelineID: "pipe-buildfail",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithBuilder(mb))
	err := exec.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("expected error from failed builder")
	}
	if run.Status != model.RunStatusFailed {
		t.Errorf("run status: got %s", run.Status)
	}
	if run.StageRuns[0].Status != model.RunStatusFailed {
		t.Errorf("stage status: got %s", run.StageRuns[0].Status)
	}
	if run.StageRuns[0].Error == "" {
		t.Error("stage run should record the error")
	}
}

func TestExecutor_DefaultBuilderIsNoop(t *testing.T) {
	// No WithBuilder → Noop. Build stage should still succeed.
	p := &model.Pipeline{
		ID: "pipe-default",
		Stages: []model.Stage{
			{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Context: "/tmp/ctx",
				Tags:    []string{"app:v1"},
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-default",
		PipelineID: "pipe-default",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}

	exec := NewExecutor()
	if err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != model.RunStatusSuccess {
		t.Errorf("run status: got %s", run.Status)
	}
	if len(run.StageRuns[0].Artifacts) != 1 {
		t.Errorf("noop should still emit an artifact; got %d", len(run.StageRuns[0].Artifacts))
	}
}
