package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cooker-ci/cooker/internal/builder"
	"github.com/cooker-ci/cooker/internal/deployer"
	"github.com/cooker-ci/cooker/internal/gitops"
	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/pusher"
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

type mockPusher struct {
	mu    sync.Mutex
	calls []pusher.Request
	res   pusher.Result
	err   error
}

func (m *mockPusher) Push(_ context.Context, req pusher.Request) (pusher.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.err != nil {
		return pusher.Result{}, m.err
	}
	return m.res, nil
}

type mockDeployer struct {
	mu    sync.Mutex
	calls []deployer.Request
	res   deployer.Result
	err   error
}

func (m *mockDeployer) Deploy(_ context.Context, req deployer.Request) (deployer.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.err != nil {
		return deployer.Result{}, m.err
	}
	return m.res, nil
}

type mockGitOps struct {
	mu    sync.Mutex
	calls []gitops.Request
	res   gitops.Result
	err   error
}

func (m *mockGitOps) Commit(_ context.Context, req gitops.Request) (gitops.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.err != nil {
		return gitops.Result{}, m.err
	}
	return m.res, nil
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
			{ID: "s4", Name: "Deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{ManifestPath: "k8s/app.yaml"}},
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

func TestExecutor_PushStage_DispatchesToPusher(t *testing.T) {
	mp := &mockPusher{res: pusher.Result{Digest: "sha256:cafef00d"}}

	p := &model.Pipeline{
		ID: "pipe-push",
		Stages: []model.Stage{
			{ID: "p", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Image:      "app:v1",
				Registry:   "registry.example.com",
				Repository: "team/app:v1",
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-push",
		PipelineID: "pipe-push",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "p", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithPusher(mp))
	if err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(mp.calls))
	}
	req := mp.calls[0]
	if req.Source != "app:v1" {
		t.Errorf("Source: got %q", req.Source)
	}
	if req.Target != "registry.example.com/team/app:v1" {
		t.Errorf("Target: got %q", req.Target)
	}
	sr := run.StageRuns[0]
	if len(sr.Artifacts) != 1 || sr.Artifacts[0].Digest != "sha256:cafef00d" {
		t.Errorf("artifact: got %+v", sr.Artifacts)
	}
}

func TestExecutor_PushStage_SkipsRegistryPrefixWhenAlreadyQualified(t *testing.T) {
	mp := &mockPusher{res: pusher.Result{Digest: "sha256:aa"}}
	p := &model.Pipeline{
		ID: "pipe-push2",
		Stages: []model.Stage{
			{ID: "p", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Image:      "app:v1",
				Registry:   "registry.example.com",
				Repository: "ghcr.io/org/app:v1",
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-push2",
		PipelineID: "pipe-push2",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "p", Status: model.RunStatusPending}},
	}
	if err := NewExecutor(WithPusher(mp)).Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mp.calls[0].Target != "ghcr.io/org/app:v1" {
		t.Errorf("already-qualified repository should not be re-prefixed; got %q", mp.calls[0].Target)
	}
}

func TestExecutor_DeployStage_DispatchesToDeployer(t *testing.T) {
	md := &mockDeployer{res: deployer.Result{AppliedResources: []string{"deployment.apps/web"}}}

	p := &model.Pipeline{
		ID: "pipe-deploy",
		Stages: []model.Stage{
			{ID: "d", Name: "Deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{
				Namespace:    "prod",
				ManifestPath: "k8s/web.yaml",
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-deploy",
		PipelineID: "pipe-deploy",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "d", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithDeployer(md))
	if err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(md.calls) != 1 {
		t.Fatalf("expected 1 deploy call, got %d", len(md.calls))
	}
	req := md.calls[0]
	if req.Kind != deployer.KindManifest {
		t.Errorf("Kind: got %q", req.Kind)
	}
	if req.Namespace != "prod" {
		t.Errorf("Namespace: got %q", req.Namespace)
	}
	sr := run.StageRuns[0]
	if len(sr.Artifacts) != 1 || sr.Artifacts[0].Ref != "deployment.apps/web" {
		t.Errorf("artifact: got %+v", sr.Artifacts)
	}
}

func TestExecutor_DeployStage_RequiresManifestOrHelm(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-deploy-empty",
		Stages: []model.Stage{
			{ID: "d", Name: "Deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{Namespace: "prod"}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-deploy-empty",
		PipelineID: "pipe-deploy-empty",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "d", Status: model.RunStatusPending}},
	}
	err := NewExecutor().Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("expected error for deploy stage missing manifest and helm")
	}
}

func TestExecutor_GitOpsCommit_DispatchesToWriter(t *testing.T) {
	mg := &mockGitOps{res: gitops.Result{CommitSHA: "abc1234"}}

	p := &model.Pipeline{
		ID: "pipe-go",
		Stages: []model.Stage{
			{ID: "g", Name: "GitOps", Type: model.StageTypeGitOpsCommit, Config: model.StageConfig{
				GitOpsRepo:    "git@github.com:org/gitops.git",
				GitOpsBranch:  "main",
				GitOpsPath:    "envs/prod/values.yaml",
				GitOpsContent: "image: ${IMAGE}",
				GitOpsMessage: "deploy prod",
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-go",
		PipelineID: "pipe-go",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "g", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithGitOps(mg))
	if err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mg.calls) != 1 {
		t.Fatalf("expected 1 gitops call, got %d", len(mg.calls))
	}
	sr := run.StageRuns[0]
	if len(sr.Artifacts) != 1 || sr.Artifacts[0].Digest != "abc1234" {
		t.Errorf("artifact: got %+v", sr.Artifacts)
	}
	if sr.Artifacts[0].Ref != "git@github.com:org/gitops.git@main" {
		t.Errorf("ref: got %q", sr.Artifacts[0].Ref)
	}
}

func TestExecutor_GitOpsCommit_RequiresRepoAndPath(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-go-bad",
		Stages: []model.Stage{
			{ID: "g", Name: "GitOps", Type: model.StageTypeGitOpsCommit, Config: model.StageConfig{}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-go-bad",
		PipelineID: "pipe-go-bad",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "g", Status: model.RunStatusPending}},
	}
	if err := NewExecutor().Execute(context.Background(), p, run); err == nil {
		t.Fatal("expected validation error")
	}
}
