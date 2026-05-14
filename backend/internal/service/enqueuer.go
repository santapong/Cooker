package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/santapong/cooker/internal/jobqueue"
)

// JobQueueEnqueuer implements the handler.JobEnqueuer interface by
// inserting a pipeline-run job into the durable queue. The matching
// worker (JobQueueRunner.Handle) picks it up via Dequeue.
//
// Concurrency-key is set to "pipeline:<id>" so two simultaneous
// triggers for the same pipeline are serialised at the queue layer
// (only one in 'running' at a time). Concurrent runs of *different*
// pipelines are unaffected.
type JobQueueEnqueuer struct {
	store jobqueue.Store
	// MaxAttempts caps retries per run. Default 1 (no retry) matches
	// the existing inline path: a failed run is failed; the user
	// can re-trigger explicitly. Operators who want auto-retry
	// (e.g., to ride out a 30-second registry outage) can raise it.
	MaxAttempts int
}

// NewJobQueueEnqueuer wraps a jobqueue.Store.
func NewJobQueueEnqueuer(store jobqueue.Store) *JobQueueEnqueuer {
	return &JobQueueEnqueuer{store: store, MaxAttempts: 1}
}

// EnqueueRun implements handler.JobEnqueuer.
func (e *JobQueueEnqueuer) EnqueueRun(ctx context.Context, pipelineID, runID string) error {
	payload, err := json.Marshal(runJobPayload{PipelineID: pipelineID, RunID: runID})
	if err != nil {
		return fmt.Errorf("enqueuer: marshal payload: %w", err)
	}
	_, err = e.store.Enqueue(ctx, JobKindPipelineRun, payload, jobqueue.EnqueueOptions{
		MaxAttempts:    e.MaxAttempts,
		ConcurrencyKey: "pipeline:" + pipelineID,
	})
	return err
}
