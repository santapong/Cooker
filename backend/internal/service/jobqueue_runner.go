package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/notify/notifier"
	"github.com/santapong/cooker/internal/store"
)

// JobKindPipelineRun is the Job.Kind string the worker registry
// binds to JobQueueRunner.Handle. Exposed as a constant so the
// enqueuer and the boot wiring use the same literal.
const JobKindPipelineRun = "pipeline-run"

// JobQueueRunner is the worker-side counterpart to the inline
// RunPipeline path: a worker picks up a job, JobQueueRunner.Handle
// loads the pipeline + run from the store, runs the executor,
// persists the result, and optionally dispatches a notification
// event. The run row is *always* updated (success or failure) so
// the existing /api/v1/pipelines/:id/runs/:runId polling endpoint
// reflects the outcome.
type JobQueueRunner struct {
	store      *store.Store
	executor   *Executor
	dispatcher *notifier.Dispatcher // optional; nil = no notifications
}

// NewJobQueueRunner constructs a runner. dispatcher may be nil when
// no notifier is wired (in which case the Handle path skips the
// dispatch step entirely).
func NewJobQueueRunner(st *store.Store, exec *Executor, disp *notifier.Dispatcher) *JobQueueRunner {
	return &JobQueueRunner{store: st, executor: exec, dispatcher: disp}
}

// runJobPayload is the wire shape carried by a pipeline-run job.
// Encoded by service.JobQueueEnqueuer and decoded here.
type runJobPayload struct {
	PipelineID string `json:"pipelineId"`
	RunID      string `json:"runId"`
}

// Handle is the jobqueue.HandlerFunc registered for JobKindPipelineRun.
// Returning nil signals success; returning an error triggers the
// worker's Reschedule path (or terminal Fail when attempts hit
// max_attempts).
//
// Important: a successful Execute that returns a failed run.Status
// is NOT a job-handler error — the *run* failed, but the *job* did
// what it was supposed to do (run the executor, persist the result).
// Only infrastructure-level problems (store load failure, payload
// decode failure, executor panic) should bubble as job errors.
func (r *JobQueueRunner) Handle(ctx context.Context, job *jobqueue.Job) error {
	var payload runJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("jobqueue runner: decode payload: %w", err)
	}
	if payload.PipelineID == "" || payload.RunID == "" {
		return fmt.Errorf("jobqueue runner: missing pipelineId/runId in payload")
	}
	p, err := r.store.Pipelines.Get(ctx, payload.PipelineID)
	if err != nil {
		return fmt.Errorf("jobqueue runner: get pipeline %s: %w", payload.PipelineID, err)
	}
	run, err := r.store.Runs.Get(ctx, payload.RunID)
	if err != nil {
		return fmt.Errorf("jobqueue runner: get run %s: %w", payload.RunID, err)
	}

	// Execute. Per F2 (handler-layering audit), Execute owns terminal
	// status transitions; we persist the run as Execute returned it.
	// A per-pipeline RunDeadline bounds the execution the same way the
	// inline coordinator path does; without one the job inherits the
	// worker's ctx as before.
	execErr := error(nil)
	if r.executor != nil {
		execCtx := ctx
		if d := PipelineRunDeadline(p); d > 0 {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
		_, execErr = r.executor.Execute(execCtx, p, run)
	}
	if updateErr := r.store.Runs.Update(ctx, run); updateErr != nil {
		// Persist failure is logged but doesn't override the executor
		// error: callers care about the executor outcome first. The
		// orphan sweep will eventually fail-out the row if Update
		// continues to fail.
		slog.Warn("jobqueue runner: update run failed", "run", run.ID, "err", updateErr)
	}

	r.dispatchOutcome(ctx, p, run, execErr)

	// execErr surfaces to the job framework only when it's an
	// infrastructure problem we want to retry. Today the executor
	// returns nil for normal failure (run.Status carries the bad
	// news); a non-nil execErr means an unexpected runtime issue,
	// which is worth a retry.
	return execErr
}

// dispatchOutcome delegates to the shared NotifyRunOutcome helper so
// the queue path and the inline Spawn path (handler.RunPipeline)
// emit identically. A run flows through exactly one of the two
// paths, so there is no double-send. NotifyRunOutcome handles the
// nil-dispatcher and non-terminal-status no-ops.
func (r *JobQueueRunner) dispatchOutcome(_ context.Context, p *model.Pipeline, run *model.PipelineRun, execErr error) {
	NotifyRunOutcome(r.dispatcher, p, run, execErr)
}
