package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/notifier"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

// simpleBuildPushPipeline returns a minimal build->push pipeline (no
// deploy stage, no k8s/docker required) whose PipelineRun is seeded to
// match, so Execute succeeds hermetically against the default Noop
// builder/pusher/deployer.
func simpleBuildPushPipeline(pipelineID, runID string) (*model.Pipeline, *model.PipelineRun) {
	p := &model.Pipeline{
		ID: pipelineID,
		Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Dockerfile: "Dockerfile", Context: ".", Tags: []string{"latest"},
			}},
			{ID: "push", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Image: "app:latest", Repository: "registry.example.com/app:latest",
			}},
		},
		Edges: []model.Edge{{ID: "e1", Source: "build", Target: "push"}},
	}
	run := &model.PipelineRun{
		ID:         runID,
		PipelineID: pipelineID,
		Status:     model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusPending},
			{StageID: "push", Status: model.RunStatusPending},
		},
	}
	return p, run
}

func mustPayload(t *testing.T, pipelineID, runID string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(runJobPayload{PipelineID: pipelineID, RunID: runID})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

func TestJobQueueRunner_Handle_HappyPath(t *testing.T) {
	st := memory.New()
	ctx := context.Background()

	p, run := simpleBuildPushPipeline("pipe-1", "run-1")
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	r := NewJobQueueRunner(st, NewExecutor(), nil)
	job := &jobqueue.Job{Payload: mustPayload(t, "pipe-1", "run-1")}

	if err := r.Handle(ctx, job); err != nil {
		t.Fatalf("Handle: unexpected error: %v", err)
	}

	got, err := st.Runs.Get(ctx, "run-1")
	if err != nil {
		t.Fatalf("get persisted run: %v", err)
	}
	if got.Status != model.RunStatusSuccess {
		t.Errorf("run status = %s, want success", got.Status)
	}
	if got.FinishedAt == nil {
		t.Error("expected FinishedAt to be stamped")
	}
}

func TestJobQueueRunner_Handle_WithDispatcherNoTargets(t *testing.T) {
	st := memory.New()
	ctx := context.Background()

	p, run := simpleBuildPushPipeline("pipe-disp", "run-disp")
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}

	disp := notifier.NewDispatcher(notifier.NewMemoryTargetStore(), notifier.NewRegistry())
	r := NewJobQueueRunner(st, NewExecutor(), disp)
	job := &jobqueue.Job{Payload: mustPayload(t, "pipe-disp", "run-disp")}

	if err := r.Handle(ctx, job); err != nil {
		t.Fatalf("Handle with a wired (empty) dispatcher: unexpected error: %v", err)
	}
}

func TestJobQueueRunner_Handle_DecodePayloadFailure(t *testing.T) {
	st := memory.New()
	r := NewJobQueueRunner(st, NewExecutor(), nil)
	job := &jobqueue.Job{Payload: json.RawMessage(`{not valid json`)}

	err := r.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "jobqueue runner: decode payload:") {
		t.Errorf("error = %q, want wrapped decode-payload prefix", err.Error())
	}
}

func TestJobQueueRunner_Handle_MissingIDs(t *testing.T) {
	tests := []struct {
		name       string
		pipelineID string
		runID      string
	}{
		{"missing pipelineId", "", "run-1"},
		{"missing runId", "pipe-1", ""},
		{"missing both", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := memory.New()
			r := NewJobQueueRunner(st, NewExecutor(), nil)
			job := &jobqueue.Job{Payload: mustPayload(t, tt.pipelineID, tt.runID)}

			err := r.Handle(context.Background(), job)
			if err == nil {
				t.Fatal("expected missing-IDs error")
			}
			if !strings.Contains(err.Error(), "missing pipelineId/runId") {
				t.Errorf("error = %q, want missing-IDs message", err.Error())
			}
		})
	}
}

func TestJobQueueRunner_Handle_PipelineGetFailure(t *testing.T) {
	st := memory.New()
	// Run exists, but its pipeline never does -- Handle loads the
	// pipeline first, so this should fail there.
	_, run := simpleBuildPushPipeline("pipe-ghost", "run-1")
	if err := st.Runs.Create(context.Background(), run); err != nil {
		t.Fatal(err)
	}

	r := NewJobQueueRunner(st, NewExecutor(), nil)
	job := &jobqueue.Job{Payload: mustPayload(t, "pipe-ghost", "run-1")}

	err := r.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected pipeline-get error")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("error = %v, want errors.Is(..., store.ErrNotFound)", err)
	}
	if !strings.Contains(err.Error(), "get pipeline") {
		t.Errorf("error = %q, want \"get pipeline\" in message", err.Error())
	}
}

func TestJobQueueRunner_Handle_RunGetFailure(t *testing.T) {
	st := memory.New()
	p, _ := simpleBuildPushPipeline("pipe-1", "run-ghost")
	if err := st.Pipelines.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	// Run is never created.

	r := NewJobQueueRunner(st, NewExecutor(), nil)
	job := &jobqueue.Job{Payload: mustPayload(t, "pipe-1", "run-ghost")}

	err := r.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected run-get error")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("error = %v, want errors.Is(..., store.ErrNotFound)", err)
	}
	if !strings.Contains(err.Error(), "get run") {
		t.Errorf("error = %q, want \"get run\" in message", err.Error())
	}
}

// errOnUpdateRuns wraps a real RunStore and forces every Update call to
// fail, while passing every other method through unchanged. Used to
// exercise Handle's "persist failure is logged but doesn't override the
// executor error" branch (jobqueue_runner.go: Runs.Update error path).
type errOnUpdateRuns struct {
	store.RunStore
	updateErr error
}

func (e *errOnUpdateRuns) Update(ctx context.Context, r *model.PipelineRun) error {
	if e.updateErr != nil {
		return e.updateErr
	}
	return e.RunStore.Update(ctx, r)
}

func TestJobQueueRunner_Handle_RunUpdateFailure(t *testing.T) {
	st := memory.New()
	ctx := context.Background()

	p, run := simpleBuildPushPipeline("pipe-1", "run-1")
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}

	// Swap in a decorator that fails every Update call, so Handle's
	// executor path succeeds but persisting the result fails.
	underlying := st.Runs
	st.Runs = &errOnUpdateRuns{RunStore: underlying, updateErr: errors.New("boom: update failed")}

	r := NewJobQueueRunner(st, NewExecutor(), nil)
	job := &jobqueue.Job{Payload: mustPayload(t, "pipe-1", "run-1")}

	// Per the documented contract (jobqueue_runner.go: "Persist failure
	// is logged but doesn't override the executor error"), Handle must
	// still return nil here because Execute succeeded -- only execErr
	// surfaces to the job framework, never the Update error.
	if err := r.Handle(ctx, job); err != nil {
		t.Fatalf("Handle should swallow the Update error, got: %v", err)
	}

	// The decorator swallowed the real Update call, so the underlying
	// store's row was never actually updated -- it's stuck at Pending.
	got, err := underlying.Get(ctx, "run-1")
	if err != nil {
		t.Fatalf("get underlying run: %v", err)
	}
	if got.Status != model.RunStatusPending {
		t.Errorf("underlying run status = %s, want it to remain Pending (Update was never really applied)", got.Status)
	}
}

func TestJobQueueRunner_Handle_ExecuteFailure_BuilderError(t *testing.T) {
	st := memory.New()
	ctx := context.Background()

	p, run := simpleBuildPushPipeline("pipe-1", "run-1")
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("builder boom")
	mb := &mockBuilder{err: wantErr}
	exec := NewExecutor(WithBuilder(mb))

	r := NewJobQueueRunner(st, exec, nil)
	job := &jobqueue.Job{Payload: mustPayload(t, "pipe-1", "run-1")}

	// A stage (builder) failure DOES bubble as a non-nil execErr from
	// Execute (confirmed by executor_test.go's
	// TestExecutor_BuildStage_BuilderErrorFailsStage) -- despite the
	// comment above Handle's execErr-return line implying that "today
	// the executor returns nil for normal failure." That comment
	// appears stale relative to actual Execute behavior; this test
	// documents and asserts the real contract rather than the
	// aspirational one.
	err := r.Handle(ctx, job)
	if err == nil {
		t.Fatal("expected Handle to surface the executor's non-nil error")
	}

	got, gerr := st.Runs.Get(ctx, "run-1")
	if gerr != nil {
		t.Fatalf("get persisted run: %v", gerr)
	}
	if got.Status != model.RunStatusFailed {
		t.Errorf("run status = %s, want failed", got.Status)
	}
}

func TestJobQueueRunner_Handle_ExecuteFailure_DAGBuildError(t *testing.T) {
	st := memory.New()
	ctx := context.Background()

	// A cyclic edge set fails BuildDAGFromPipeline before any stage
	// runs, deterministically and hermetically (zero I/O) producing a
	// non-nil execErr wrapped "building DAG: ...".
	p := &model.Pipeline{
		ID: "pipe-cycle",
		Stages: []model.Stage{
			{ID: "a", Name: "A", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"a"}}},
			{ID: "b", Name: "B", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"b"}}},
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
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}

	r := NewJobQueueRunner(st, NewExecutor(), nil)
	job := &jobqueue.Job{Payload: mustPayload(t, "pipe-cycle", "run-cycle")}

	err := r.Handle(ctx, job)
	if err == nil {
		t.Fatal("expected Handle to surface a DAG-build error")
	}
	if !strings.Contains(err.Error(), "building DAG") {
		t.Errorf("error = %q, want it to mention DAG construction", err.Error())
	}

	got, gerr := st.Runs.Get(ctx, "run-cycle")
	if gerr != nil {
		t.Fatalf("get persisted run: %v", gerr)
	}
	if got.Status != model.RunStatusFailed {
		t.Errorf("run status = %s, want failed", got.Status)
	}
}
