package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/build/stagerunner"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

// fakeStageRunner records every Run call and returns a configured result.
// Safe for concurrent use.
type fakeStageRunner struct {
	mu       sync.Mutex
	calls    []stagerunner.Request
	exitCode int
	err      error
}

func (f *fakeStageRunner) Run(_ context.Context, req stagerunner.Request) (stagerunner.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if f.err != nil {
		return stagerunner.Result{}, f.err
	}
	if req.LogWriter != nil {
		_, _ = req.LogWriter.Write([]byte("ran in " + req.Image + "\n"))
	}
	return stagerunner.Result{ExitCode: f.exitCode}, nil
}

func (f *fakeStageRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func singleStagePipeline(stageType model.StageType, cfg model.StageConfig) (*model.Pipeline, *model.PipelineRun) {
	p := &model.Pipeline{
		ID: "pipe-" + string(stageType),
		Stages: []model.Stage{
			{ID: "s1", Name: string(stageType), Type: stageType, Config: cfg},
		},
		Edges: []model.Edge{},
	}
	run := &model.PipelineRun{
		ID:         "run-" + string(stageType),
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "s1", Status: model.RunStatusPending}},
	}
	return p, run
}

func TestExecutor_TestStage_SuccessRunsContainer(t *testing.T) {
	runner := &fakeStageRunner{exitCode: 0}
	exec := NewExecutor(WithStageRunner(runner))
	p, run := singleStagePipeline(model.StageTypeTest, model.StageConfig{
		Image:   "node:20-alpine",
		Command: []string{"npm", "test"},
	})

	res, err := exec.Execute(context.Background(), p, run)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != model.RunStatusSuccess {
		t.Fatalf("run status = %s, want success", res.Status)
	}
	if runner.callCount() != 1 {
		t.Fatalf("runner called %d times, want 1", runner.callCount())
	}
	got := runner.calls[0]
	if got.Image != "node:20-alpine" {
		t.Errorf("runner image = %q, want node:20-alpine", got.Image)
	}
	if strings.Join(got.Command, " ") != "npm test" {
		t.Errorf("runner command = %v, want [npm test]", got.Command)
	}
	if !strings.Contains(run.StageRuns[0].Logs, "ran in node:20-alpine") {
		t.Errorf("stage logs not captured: %q", run.StageRuns[0].Logs)
	}
}

func TestExecutor_TestStage_NonZeroExitFailsStage(t *testing.T) {
	runner := &fakeStageRunner{exitCode: 7}
	exec := NewExecutor(WithStageRunner(runner))
	p, run := singleStagePipeline(model.StageTypeTest, model.StageConfig{Image: "golang:1.22"})

	_, err := exec.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("expected error from non-zero exit, got nil")
	}
	if run.Status != model.RunStatusFailed {
		t.Errorf("run status = %s, want failed", run.Status)
	}
	if !strings.Contains(run.StageRuns[0].Error, "exited 7") {
		t.Errorf("stage error = %q, want it to mention exit 7", run.StageRuns[0].Error)
	}
}

func TestExecutor_CustomStage_WrapsScript(t *testing.T) {
	runner := &fakeStageRunner{exitCode: 0}
	exec := NewExecutor(WithStageRunner(runner))
	p, run := singleStagePipeline(model.StageTypeCustom, model.StageConfig{
		Image:  "alpine:3.20",
		Script: "echo hello",
	})

	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if runner.callCount() != 1 {
		t.Fatalf("runner called %d times, want 1", runner.callCount())
	}
	if runner.calls[0].Script != "echo hello" {
		t.Errorf("runner script = %q, want 'echo hello'", runner.calls[0].Script)
	}
}

func TestExecutor_CustomStage_RequiresImage(t *testing.T) {
	runner := &fakeStageRunner{exitCode: 0}
	exec := NewExecutor(WithStageRunner(runner))
	p, run := singleStagePipeline(model.StageTypeCustom, model.StageConfig{Script: "echo hi"})

	_, err := exec.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("custom stage with no image should fail")
	}
	if runner.callCount() != 0 {
		t.Errorf("runner should not be called for a misconfigured stage, got %d calls", runner.callCount())
	}
}

func TestExecutor_RuntimeStage_ContextCancelFailsStage(t *testing.T) {
	// A runner that blocks until ctx is cancelled, then returns ctx.Err().
	blockRunner := stageRunnerFunc(func(ctx context.Context, _ stagerunner.Request) (stagerunner.Result, error) {
		<-ctx.Done()
		return stagerunner.Result{}, ctx.Err()
	})
	exec := NewExecutor(WithStageRunner(blockRunner))
	p, run := singleStagePipeline(model.StageTypeTest, model.StageConfig{Image: "busybox"})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := exec.Execute(ctx, p, run)
	if err == nil {
		t.Fatal("expected error when ctx is cancelled mid-stage")
	}
	// A cancelled run finalizes to Cancelled (or Failed if the cancel
	// raced after the stage); either is a non-success terminal state.
	if run.Status == model.RunStatusSuccess {
		t.Errorf("run status = success, want a non-success terminal state")
	}
}

// stageRunnerFunc adapts a func to the stagerunner.Runner interface.
type stageRunnerFunc func(context.Context, stagerunner.Request) (stagerunner.Result, error)

func (f stageRunnerFunc) Run(ctx context.Context, req stagerunner.Request) (stagerunner.Result, error) {
	return f(ctx, req)
}

// --- Approval gate ---

func newApprovalExecutor(t *testing.T) (*Executor, *StageApprovalService) {
	t.Helper()
	st := memory.New()
	svc := NewStageApprovalService(st.StageApprovals)
	exec := NewExecutor(WithStageApprovals(svc))
	// Poll fast so the gate test doesn't take seconds.
	exec.approvalPoll = 5 * time.Millisecond
	return exec, svc
}

func TestExecutor_ApprovalGate_ApproveResumes(t *testing.T) {
	exec, svc := newApprovalExecutor(t)
	p, run := singleStagePipeline(model.StageTypeApproval, model.StageConfig{RequiredApprovers: 1})

	// Approve out-of-band once the gate has been opened.
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if g, err := svc.Get(context.Background(), run.ID, "s1"); err == nil && g.Status == model.StageApprovalAwaiting {
				_, _ = svc.Approve(context.Background(), run.ID, "s1", "sub-1", "approver@x.com", "lgtm")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	res, err := exec.Execute(context.Background(), p, run)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != model.RunStatusSuccess {
		t.Fatalf("run status = %s, want success", res.Status)
	}
	g, err := svc.Get(context.Background(), run.ID, "s1")
	if err != nil {
		t.Fatalf("get gate: %v", err)
	}
	if g.Status != model.StageApprovalApproved {
		t.Errorf("gate status = %s, want approved", g.Status)
	}
}

func TestExecutor_ApprovalGate_RejectFailsStage(t *testing.T) {
	exec, svc := newApprovalExecutor(t)
	p, run := singleStagePipeline(model.StageTypeApproval, model.StageConfig{})

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if g, err := svc.Get(context.Background(), run.ID, "s1"); err == nil && g.Status == model.StageApprovalAwaiting {
				_, _ = svc.Reject(context.Background(), run.ID, "s1", "approver@x.com", "no")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	_, err := exec.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("expected error when gate is rejected")
	}
	if run.Status != model.RunStatusFailed {
		t.Errorf("run status = %s, want failed", run.Status)
	}
	if !strings.Contains(run.StageRuns[0].Error, "rejected") {
		t.Errorf("stage error = %q, want it to mention rejection", run.StageRuns[0].Error)
	}
}

func TestExecutor_ApprovalGate_ContextCancelFailsStage(t *testing.T) {
	exec, _ := newApprovalExecutor(t)
	p, run := singleStagePipeline(model.StageTypeApproval, model.StageConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_, err := exec.Execute(ctx, p, run)
	if err == nil {
		t.Fatal("expected error when ctx is cancelled while gate is awaiting")
	}
	if run.Status == model.RunStatusSuccess {
		t.Errorf("run status = success, want a non-success terminal state")
	}
}

func TestExecutor_ApprovalGate_NoServiceFailsLoud(t *testing.T) {
	// No StageApprovalService wired: the gate cannot be persisted, so the
	// stage must fail rather than silently auto-pass a human gate.
	exec := NewExecutor()
	p, run := singleStagePipeline(model.StageTypeApproval, model.StageConfig{})

	_, err := exec.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("approval stage with no gate service should fail loud")
	}
	if run.Status != model.RunStatusFailed {
		t.Errorf("run status = %s, want failed", run.Status)
	}
}
