package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/builder"
	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/gitops"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/pusher"
)

// fakeRunUpdater records every persistProgress call. Safe for concurrent use.
type fakeRunUpdater struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeRunUpdater) update(_ context.Context, _ *model.PipelineRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return nil
}

func (f *fakeRunUpdater) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

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
	// Use implemented stage types only (build → push). The test stage type
	// is deliberately unimplemented and returns an error (T1 fix); using it
	// here would make this pipeline-level test fail for the wrong reason.
	p := &model.Pipeline{
		ID: "pipe-1",
		Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Dockerfile: "Dockerfile",
				Context:    ".",
				Tags:       []string{"latest"},
			}},
			{ID: "push", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Image:      "app:latest",
				Repository: "registry.example.com/app:latest",
			}},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "push"},
		},
	}

	run := &model.PipelineRun{
		ID:         "run-1",
		PipelineID: "pipe-1",
		Status:     model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusPending},
			{StageID: "push", Status: model.RunStatusPending},
		},
	}

	executor := NewExecutor()
	result, err := executor.Execute(context.Background(), p, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != model.RunStatusSuccess {
		t.Errorf("expected result status success, got %s", result.Status)
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

// TestExecutor_ImplementedStageTypes verifies the three fully-implemented
// stage types (build, push, deploy) succeed end-to-end. test/approval/custom
// are stubs that now return errors — verified separately in
// TestExecutor_StubStagesFail.
func TestExecutor_ImplementedStageTypes(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-implemented",
		Stages: []model.Stage{
			{ID: "s1", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{}},
			{ID: "s2", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{}},
			{ID: "s3", Name: "Deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{ManifestPath: "k8s/app.yaml"}},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "s1", Target: "s2"},
			{ID: "e2", Source: "s2", Target: "s3"},
		},
	}

	stageRuns := make([]model.StageRun, len(p.Stages))
	for i, s := range p.Stages {
		stageRuns[i] = model.StageRun{StageID: s.ID, Status: model.RunStatusPending}
	}

	run := &model.PipelineRun{
		ID:         "run-implemented",
		PipelineID: "pipe-implemented",
		Status:     model.RunStatusPending,
		StageRuns:  stageRuns,
	}

	executor := NewExecutor()
	_, err := executor.Execute(context.Background(), p, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if run.Status != model.RunStatusSuccess {
		t.Errorf("expected success, got %s", run.Status)
	}
}

// TestExecutor_StubStagesFail verifies that the unimplemented stage types
// (test, approval, custom) return an explicit error rather than silently
// returning nil. Closes dag-adaptation-2026.md §6 T1.
func TestExecutor_StubStagesFail(t *testing.T) {
	stubTypes := []model.StageType{
		model.StageTypeTest,
		model.StageTypeApproval,
		model.StageTypeCustom,
	}

	for _, st := range stubTypes {
		st := st
		t.Run(string(st), func(t *testing.T) {
			p := &model.Pipeline{
				ID: "pipe-stub-" + string(st),
				Stages: []model.Stage{
					{ID: "s1", Name: string(st), Type: st, Config: model.StageConfig{}},
				},
				Edges: []model.Edge{},
			}
			run := &model.PipelineRun{
				ID:         "run-stub-" + string(st),
				PipelineID: p.ID,
				Status:     model.RunStatusPending,
				StageRuns:  []model.StageRun{{StageID: "s1", Status: model.RunStatusPending}},
			}

			exec := NewExecutor()
			_, err := exec.Execute(context.Background(), p, run)
			if err == nil {
				t.Fatalf("stage type %q should return an error (not implemented), got nil", st)
			}
			if run.Status != model.RunStatusFailed {
				t.Errorf("expected run status failed, got %s", run.Status)
			}
			if run.StageRuns[0].Error == "" {
				t.Error("expected non-empty StageRun.Error field")
			}
		})
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
	_, err := executor.Execute(context.Background(), p, run)
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
	_, err := executor.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("expected error for cyclic DAG")
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-cancel",
		Stages: []model.Stage{
			{ID: "s1", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{}},
			{ID: "s2", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{}},
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
	_, err := executor.Execute(ctx, p, run)
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
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
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
	_, err := exec.Execute(context.Background(), p, run)
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
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
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
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
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
	if _, err := NewExecutor(WithPusher(mp)).Execute(context.Background(), p, run); err != nil {
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
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
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
	_, err := NewExecutor().Execute(context.Background(), p, run)
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
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
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
	if _, err := NewExecutor().Execute(context.Background(), p, run); err == nil {
		t.Fatal("expected validation error")
	}
}

// TestExecutor_T5_BatchedWritesDebounce verifies that non-terminal stage
// transitions are batched: the total number of RunUpdater calls must be
// strictly less than the pre-T5 count (one call per transition).
//
// A 12-stage linear pipeline (all Noop build stages) emits 2 transitions per
// stage (runner emits "running" then "success") = 24 total. T5 must absorb
// these into fewer writes via the min(500ms, 10 transitions) debounce.
//
// Refs: dag-adaptation-2026.md §6 T5; docs/audits/2026-05-p1-context-pack.md.
func TestExecutor_T5_BatchedWritesDebounce(t *testing.T) {
	const numStages = 12 // > progressDrainBatchSize to exercise batching

	stages := make([]model.Stage, numStages)
	stageRuns := make([]model.StageRun, numStages)
	edges := make([]model.Edge, numStages-1)

	ids := [12]string{"sa", "sb", "sc", "sd", "se", "sf", "sg", "sh", "si", "sj", "sk", "sl"}
	for i := 0; i < numStages; i++ {
		id := ids[i]
		stages[i] = model.Stage{
			ID:   id,
			Name: "Stage " + id,
			Type: model.StageTypeBuild,
			Config: model.StageConfig{
				Context: ".",
				Tags:    []string{"app:latest"},
			},
		}
		stageRuns[i] = model.StageRun{StageID: id, Status: model.RunStatusPending}
		if i > 0 {
			edges[i-1] = model.Edge{
				ID:     "e" + id,
				Source: ids[i-1],
				Target: id,
			}
		}
	}

	p := &model.Pipeline{
		ID:     "pipe-t5-debounce",
		Stages: stages,
		Edges:  edges,
	}
	run := &model.PipelineRun{
		ID:         "run-t5-debounce",
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns:  stageRuns,
	}

	updater := &fakeRunUpdater{}
	exec := NewExecutor(WithRunUpdater(updater.update))

	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != model.RunStatusSuccess {
		t.Errorf("expected run status success, got %s", run.Status)
	}

	calls := updater.callCount()
	// Pre-T5 behaviour: 2 explicit persistProgress calls per stage (start
	// set + terminal set) plus one at the "running" runner event → at least
	// 2×numStages = 24 if every transition triggered a direct write.
	// T5 must produce strictly fewer writes via batching.
	priorCallCount := 2 * numStages
	if calls >= priorCallCount {
		t.Errorf("T5 debounce: expected < %d persistProgress calls, got %d (batching not working)", priorCallCount, calls)
	}
	if calls == 0 {
		t.Error("T5 debounce: expected at least 1 persistProgress call, got 0")
	}
	t.Logf("T5 debounce: %d stages → %d persistProgress calls (pre-T5 would be ≥%d)", numStages, calls, priorCallCount)
}

// TestExecutor_T5_EagerFlushOnTerminal verifies that a terminal stage status
// ("failed") triggers an immediate persistProgress flush without waiting for
// the debounce timer or batch-size threshold.
//
// The test injects a builder that always errors. The drain goroutine must
// flush eagerly when it receives the "failed" StatusUpdate. Because Execute
// joins the drain goroutine before returning (<-drainDone), the RunUpdater
// call count must be ≥1 at the point Execute returns.
//
// Per dag-adaptation-2026.md §6 T5: "Acceptable to flush eagerly on
// Status == 'failed' or Status == 'success'". This test makes that contract
// machine-verifiable per PR #61's coordination guidance.
func TestExecutor_T5_EagerFlushOnTerminal(t *testing.T) {
	wantErr := errors.New("injected build failure")
	mb := &mockBuilder{err: wantErr}

	p := &model.Pipeline{
		ID: "pipe-t5-eager",
		Stages: []model.Stage{
			{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Context: ".",
				Tags:    []string{"app:v1"},
			}},
		},
		Edges: []model.Edge{},
	}
	run := &model.PipelineRun{
		ID:         "run-t5-eager",
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}

	updater := &fakeRunUpdater{}
	exec := NewExecutor(
		WithBuilder(mb),
		WithRunUpdater(updater.update),
	)

	// Execute must return an error (builder always errors).
	if _, execErr := exec.Execute(context.Background(), p, run); execErr == nil {
		t.Fatal("expected error from failed builder, got nil")
	}
	if run.Status != model.RunStatusFailed {
		t.Errorf("expected run status failed, got %s", run.Status)
	}

	// Eager-flush guarantee: at least one persistProgress call must have
	// been made before Execute returns. Without eager flush the drain
	// goroutine would defer the write until the 500ms tick, which may
	// not fire before the caller observes the result.
	if calls := updater.callCount(); calls == 0 {
		t.Error("T5 eager-flush: expected ≥1 persistProgress call on terminal 'failed', got 0")
	} else {
		t.Logf("T5 eager-flush: %d persistProgress call(s) on single-stage terminal failure", calls)
	}

	// Stage-level state must also be set correctly.
	if run.StageRuns[0].Status != model.RunStatusFailed {
		t.Errorf("stage status: expected failed, got %s", run.StageRuns[0].Status)
	}
	if run.StageRuns[0].Error == "" {
		t.Error("stage run should record the error string")
	}
}

// TestExecutor_F2_RunResult locks in the terminal-RunResult contract
// introduced by F2 (docs/audits/2026-05-handler-layering.md Finding 2).
// Execute now returns (model.RunResult, error) and owns the terminal-
// status state machine that previously lived in the handler closure
// at handler/pipeline.go:191–210. The handler persists the result
// verbatim — no Cancelled→Success flip, no Failed→Failed double-set.
//
// The four cases cover every terminal axis the handler used to derive:
//   - all-success
//   - one stage failed
//   - context cancelled (must NOT flip Cancelled→Success — this is
//     the silent-flip lock-in the audit flagged)
//   - empty pipeline (zero stages, no error)
func TestExecutor_F2_RunResult(t *testing.T) {
	tests := []struct {
		name        string
		buildPlan   func() (*model.Pipeline, *model.PipelineRun)
		ctxFn       func() (context.Context, context.CancelFunc)
		wantStatus  model.RunStatus
		wantErrText string // substring expected in err.Error(); empty → expect nil error
	}{
		{
			name: "all stages success",
			buildPlan: func() (*model.Pipeline, *model.PipelineRun) {
				p := &model.Pipeline{
					ID: "pipe-f2-success",
					Stages: []model.Stage{
						{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"app:v1"}}},
						{ID: "p", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{Image: "app:v1", Repository: "registry.example.com/app:v1"}},
					},
					Edges: []model.Edge{{ID: "e1", Source: "b", Target: "p"}},
				}
				run := &model.PipelineRun{
					ID:         "run-f2-success",
					PipelineID: p.ID,
					Status:     model.RunStatusPending,
					StageRuns: []model.StageRun{
						{StageID: "b", Status: model.RunStatusPending},
						{StageID: "p", Status: model.RunStatusPending},
					},
				}
				return p, run
			},
			ctxFn:      func() (context.Context, context.CancelFunc) { return context.Background(), func() {} },
			wantStatus: model.RunStatusSuccess,
		},
		{
			name: "one stage fails",
			buildPlan: func() (*model.Pipeline, *model.PipelineRun) {
				// Use a deploy stage missing both ManifestPath and HelmChart —
				// deterministic validation failure with no concurrent goroutine.
				p := &model.Pipeline{
					ID: "pipe-f2-fail",
					Stages: []model.Stage{
						{ID: "d", Name: "Deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{Namespace: "prod"}},
					},
					Edges: []model.Edge{},
				}
				run := &model.PipelineRun{
					ID:         "run-f2-fail",
					PipelineID: p.ID,
					Status:     model.RunStatusPending,
					StageRuns:  []model.StageRun{{StageID: "d", Status: model.RunStatusPending}},
				}
				return p, run
			},
			ctxFn:       func() (context.Context, context.CancelFunc) { return context.Background(), func() {} },
			wantStatus:  model.RunStatusFailed,
			wantErrText: "deploy",
		},
		{
			// F2 silent-flip lock-in: the pre-F2 handler closure did
			// `else if runCopy.Status == model.RunStatusRunning {
			// runCopy.Status = RunStatusSuccess }` which would have
			// correctly NOT overwritten Cancelled — but it would have
			// missed the case where the executor returns nil error
			// because the runner saw context.Canceled and recorded it
			// some other way. Lock the contract at the executor: a
			// context-cancelled run reports Cancelled (NOT Failed,
			// NOT Success).
			name: "context cancelled",
			buildPlan: func() (*model.Pipeline, *model.PipelineRun) {
				p := &model.Pipeline{
					ID: "pipe-f2-cancel",
					Stages: []model.Stage{
						{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"app:v1"}}},
					},
					Edges: []model.Edge{},
				}
				run := &model.PipelineRun{
					ID:         "run-f2-cancel",
					PipelineID: p.ID,
					Status:     model.RunStatusPending,
					StageRuns:  []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
				}
				return p, run
			},
			ctxFn: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // pre-cancelled — runner.Run returns immediately
				return ctx, func() {}
			},
			wantStatus: model.RunStatusCancelled,
			// Runner surfaces context.Canceled; the executor returns it
			// AND stamps Status=Cancelled. The handler discards the err
			// (Status drives the persisted row); the lock-in is the
			// terminal status, not the err shape.
			wantErrText: "context canceled",
		},
		{
			name: "empty pipeline",
			buildPlan: func() (*model.Pipeline, *model.PipelineRun) {
				p := &model.Pipeline{
					ID:     "pipe-f2-empty",
					Stages: []model.Stage{},
					Edges:  []model.Edge{},
				}
				run := &model.PipelineRun{
					ID:         "run-f2-empty",
					PipelineID: p.ID,
					Status:     model.RunStatusPending,
					StageRuns:  []model.StageRun{},
				}
				return p, run
			},
			ctxFn:      func() (context.Context, context.CancelFunc) { return context.Background(), func() {} },
			wantStatus: model.RunStatusSuccess,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			p, run := tt.buildPlan()
			ctx, cancel := tt.ctxFn()
			defer cancel()

			before := time.Now()
			result, err := NewExecutor().Execute(ctx, p, run)
			after := time.Now()

			// Error expectation.
			switch {
			case tt.wantErrText == "" && err != nil:
				t.Fatalf("expected nil error, got %v", err)
			case tt.wantErrText != "" && err == nil:
				t.Fatalf("expected error containing %q, got nil", tt.wantErrText)
			}

			// Terminal status: matches on the RunResult AND on run.Status.
			// Both must agree — F2 guarantees the in-memory PipelineRun
			// reflects the same terminal value the result carries.
			if result.Status != tt.wantStatus {
				t.Errorf("result.Status: want %q, got %q", tt.wantStatus, result.Status)
			}
			if run.Status != tt.wantStatus {
				t.Errorf("run.Status: want %q, got %q (executor must stamp the run too)", tt.wantStatus, run.Status)
			}

			// FinishedAt non-zero. The exact instant is unobservable, but
			// it MUST be in [before, after] so we can rule out the
			// default-zero or in-the-past programmer error.
			if result.FinishedAt.IsZero() {
				t.Error("result.FinishedAt must not be zero on a terminal RunResult")
			}
			if result.FinishedAt.Before(before) || result.FinishedAt.After(after) {
				t.Errorf("result.FinishedAt %v not in [%v, %v]", result.FinishedAt, before, after)
			}

			// run.FinishedAt must also be stamped — the handler closure
			// used to do this and now relies on the executor.
			if run.FinishedAt == nil {
				t.Error("run.FinishedAt must be set on a terminal run")
			} else if !run.FinishedAt.Equal(result.FinishedAt) {
				t.Errorf("run.FinishedAt %v != result.FinishedAt %v (should be the same instant)",
					*run.FinishedAt, result.FinishedAt)
			}
		})
	}
}

// --- Primitive #3: inter-stage outputs (dag-adaptation-2026.md §7.3) ---

// TestExecutor_Outputs_BuildDigestFlowsToPush is the headline case: a Build
// stage emits a digest, and a downstream Push stage references it via
// ${stages.<buildID>.digest} in its Repository. The executor must interpolate
// the token before dispatching to the pusher, so the pusher receives the
// resolved Target. Run with -race (go test ./... -race).
func TestExecutor_Outputs_BuildDigestFlowsToPush(t *testing.T) {
	mb := &mockBuilder{res: builder.Result{
		ImageID: "sha256:abcd1234",
		Tags:    []string{"reg/app:v1"},
	}}
	mp := &mockPusher{res: pusher.Result{Digest: "sha256:pushed"}}

	p := &model.Pipeline{
		ID: "pipe-outputs",
		Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Context: ".",
				Tags:    []string{"reg/app:v1"},
			}},
			{ID: "push", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Image:      "reg/app:v1",
				Repository: "reg/app@${stages.build.digest}",
			}},
		},
		Edges: []model.Edge{{ID: "e1", Source: "build", Target: "push"}},
	}
	run := &model.PipelineRun{
		ID:         "run-outputs",
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusPending},
			{StageID: "push", Status: model.RunStatusPending},
		},
	}

	exec := NewExecutor(WithBuilder(mb), WithPusher(mp))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != model.RunStatusSuccess {
		t.Fatalf("expected success, got %s", run.Status)
	}
	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(mp.calls))
	}
	if want := "reg/app@sha256:abcd1234"; mp.calls[0].Target != want {
		t.Errorf("pusher Target: got %q, want %q (digest not interpolated)", mp.calls[0].Target, want)
	}
	// The build stage's outputs must be recorded on its StageRun.
	var buildSR *model.StageRun
	for i := range run.StageRuns {
		if run.StageRuns[i].StageID == "build" {
			buildSR = &run.StageRuns[i]
		}
	}
	if buildSR == nil || buildSR.Outputs["digest"] != "sha256:abcd1234" {
		t.Errorf("build outputs.digest: got %v", buildSR.Outputs)
	}
}

// TestExecutor_Outputs_ParallelSiblingsNoRace exercises the concurrency path
// the headline test does not: two build stages with no edge between them run
// concurrently in the same DAG level, each invoking the outputs resolver at
// stage start. If the resolver reads the whole stage-run map it reads a
// sibling's StageRun.Status while that sibling's goroutine is writing it —
// a data race with no happens-before edge. The resolver must therefore only
// read ANCESTOR stage runs (fenced by the runner's per-level WaitGroup).
// Must stay green under `go test -race`.
func TestExecutor_Outputs_ParallelSiblingsNoRace(t *testing.T) {
	mb := &mockBuilder{res: builder.Result{ImageID: "sha256:abcd1234", Tags: []string{"reg/app:v1"}}}
	mp := &mockPusher{res: pusher.Result{Digest: "sha256:pushed"}}

	p := &model.Pipeline{
		ID: "pipe-parallel",
		Stages: []model.Stage{
			{ID: "b1", Name: "B1", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"reg/app:v1"}}},
			{ID: "b2", Name: "B2", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"reg/app:v1"}}},
			{ID: "push", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Image:      "reg/app:v1",
				Repository: "reg/app@${stages.b1.digest}",
			}},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "b1", Target: "push"},
			{ID: "e2", Source: "b2", Target: "push"},
		},
	}
	run := &model.PipelineRun{
		ID: "run-parallel", PipelineID: p.ID, Status: model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "b1", Status: model.RunStatusPending},
			{StageID: "b2", Status: model.RunStatusPending},
			{StageID: "push", Status: model.RunStatusPending},
		},
	}

	exec := NewExecutor(WithBuilder(mb), WithPusher(mp))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != model.RunStatusSuccess {
		t.Fatalf("expected success, got %s", run.Status)
	}
	if len(mp.calls) != 1 || mp.calls[0].Target != "reg/app@sha256:abcd1234" {
		t.Fatalf("push target: got %+v", mp.calls)
	}
}

// TestExecutor_Outputs_UnknownKeyFailsStage verifies that a reference to an
// output key the upstream stage never produced is a runtime stage failure
// carrying the buildplan error (key existence is not validated statically).
func TestExecutor_Outputs_UnknownKeyFailsStage(t *testing.T) {
	mb := &mockBuilder{res: builder.Result{ImageID: "sha256:abcd1234", Tags: []string{"reg/app:v1"}}}
	mp := &mockPusher{res: pusher.Result{Digest: "sha256:pushed"}}

	p := &model.Pipeline{
		ID: "pipe-outputs-missing",
		Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"reg/app:v1"}}},
			{ID: "push", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Image:      "reg/app:v1",
				Repository: "reg/app@${stages.build.missing}",
			}},
		},
		Edges: []model.Edge{{ID: "e1", Source: "build", Target: "push"}},
	}
	run := &model.PipelineRun{
		ID:         "run-outputs-missing",
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusPending},
			{StageID: "push", Status: model.RunStatusPending},
		},
	}

	exec := NewExecutor(WithBuilder(mb), WithPusher(mp))
	_, err := exec.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("expected stage failure for unknown output key")
	}
	if run.Status != model.RunStatusFailed {
		t.Errorf("expected run status failed, got %s", run.Status)
	}
	// The push stage must carry the buildplan error and the pusher must
	// never have been invoked (interpolation fails before dispatch).
	var pushSR *model.StageRun
	for i := range run.StageRuns {
		if run.StageRuns[i].StageID == "push" {
			pushSR = &run.StageRuns[i]
		}
	}
	if pushSR == nil || pushSR.Status != model.RunStatusFailed {
		t.Fatalf("expected push stage failed, got %+v", pushSR)
	}
	if !strings.Contains(pushSR.Error, "no output") {
		t.Errorf("expected buildplan 'no output' error, got %q", pushSR.Error)
	}
	if len(mp.calls) != 0 {
		t.Errorf("pusher must not be called when interpolation fails; got %d calls", len(mp.calls))
	}
}

// TestExecutor_Outputs_CapTruncation verifies an oversized adapter-emitted
// output value is truncated to the per-key cap and the "_truncated" marker
// is set on the stage's outputs.
func TestExecutor_Outputs_CapTruncation(t *testing.T) {
	big := strings.Repeat("x", outputKeyMax+500)
	mb := &mockBuilder{res: builder.Result{
		ImageID: "sha256:abcd1234",
		Tags:    []string{"reg/app:v1"},
		Outputs: map[string]string{"blob": big},
	}}

	p := &model.Pipeline{
		ID: "pipe-outputs-cap",
		Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"reg/app:v1"}}},
		},
		Edges: []model.Edge{},
	}
	run := &model.PipelineRun{
		ID:         "run-outputs-cap",
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "build", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithBuilder(mb))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := run.StageRuns[0].Outputs
	if out == nil {
		t.Fatal("expected outputs to be populated")
	}
	if out["_truncated"] != "true" {
		t.Errorf("expected _truncated marker, got outputs=%v", out)
	}
	if v := out["blob"]; len(v) <= outputKeyMax || len(v) > outputKeyMax+len("...[truncated]") {
		t.Errorf("expected blob truncated to ~%d bytes, got %d", outputKeyMax, len(v))
	}
}

// TestHasDisallowedControl covers the output-value sanitizer, including the
// audit-L1 hardening (C1 controls + Unicode line/paragraph separators).
func TestHasDisallowedControl(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"plain", "sha256:abcd1234", false},
		{"with space", "reg/app v1", false},
		{"with tab", "a\tb", false},
		{"newline", "a\nb", true},
		{"carriage return", "a\rb", true},
		{"nul", "a\x00b", true},
		{"escape", "a\x1bb", true},
		{"del", "a\x7fb", true},
		{"c1 control", "a\u0085b", true},        // NEL (U+0085) in the C1 range
		{"line separator", "a\u2028b", true},    // U+2028
		{"paragraph separator", "a\u2029b", true}, // U+2029
		{"unicode text", "café-π", false},
	}
	for _, tc := range cases {
		if got := hasDisallowedControl(tc.in); got != tc.want {
			t.Errorf("%s: hasDisallowedControl(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestExecutor_Outputs_DisabledPassesThrough verifies that with
// COOKER_OUTPUTS_ENABLED=false the ${stages.*} token is passed through to
// the adapter literally — no interpolation, no error, no ingestion.
func TestExecutor_Outputs_DisabledPassesThrough(t *testing.T) {
	t.Setenv("COOKER_OUTPUTS_ENABLED", "false")

	mb := &mockBuilder{res: builder.Result{ImageID: "sha256:abcd1234", Tags: []string{"reg/app:v1"}}}
	mp := &mockPusher{res: pusher.Result{Digest: "sha256:pushed"}}

	p := &model.Pipeline{
		ID: "pipe-outputs-off",
		Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"reg/app:v1"}}},
			{ID: "push", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Image:      "reg/app:v1",
				Repository: "reg/app@${stages.build.digest}",
			}},
		},
		Edges: []model.Edge{{ID: "e1", Source: "build", Target: "push"}},
	}
	run := &model.PipelineRun{
		ID:         "run-outputs-off",
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusPending},
			{StageID: "push", Status: model.RunStatusPending},
		},
	}

	exec := NewExecutor(WithBuilder(mb), WithPusher(mp))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(mp.calls))
	}
	if want := "reg/app@${stages.build.digest}"; mp.calls[0].Target != want {
		t.Errorf("with outputs disabled, token must pass through literally: got %q, want %q", mp.calls[0].Target, want)
	}
	// Ingestion is also short-circuited: no outputs stored.
	if out := run.StageRuns[0].Outputs; out != nil {
		t.Errorf("expected no outputs ingested when disabled, got %v", out)
	}
}

// TestExecutor_F2_CancelledStaysCancelled is the explicit silent-flip
// lock-in flagged in PR #55 / handler-layering audit Finding 2:
// the pre-F2 handler closure flipped run.Status to Success whenever
// Execute returned nil error and the run was still "Running".
// If a future code path concurrently sets run.Status = Cancelled
// (e.g. CancelPipelineRun race), Execute must NOT overwrite that
// terminal status to Success.
//
// We simulate the race by setting run.Status = Cancelled BEFORE
// calling Execute — equivalent to a cancellation that lands on the
// row while the executor is mid-flight. Even though every stage in
// the pipeline completes successfully and Execute returns nil error,
// the terminal status must remain Cancelled.
func TestExecutor_F2_CancelledStaysCancelled(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-f2-cancelled-locked",
		Stages: []model.Stage{
			{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Context: ".",
				Tags:    []string{"app:v1"},
			}},
		},
		Edges: []model.Edge{},
	}
	run := &model.PipelineRun{
		ID:         "run-f2-cancelled-locked",
		PipelineID: p.ID,
		// Pre-set Cancelled — emulating CancelPipelineRun racing with
		// the executor. The pre-F2 handler closure would have flipped
		// this to Success once Execute returned nil; F2 forbids that.
		Status:    model.RunStatusCancelled,
		StageRuns: []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}

	result, err := NewExecutor().Execute(context.Background(), p, run)
	if err != nil {
		t.Fatalf("expected nil error (all stages succeed), got %v", err)
	}

	if result.Status != model.RunStatusCancelled {
		t.Errorf("result.Status: want Cancelled (lock-in), got %q — F2 silent-flip regression", result.Status)
	}
	if run.Status != model.RunStatusCancelled {
		t.Errorf("run.Status: want Cancelled (lock-in), got %q — F2 silent-flip regression", run.Status)
	}
}
