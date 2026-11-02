package service

import (
	"context"
	"encoding/json"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/notify/notifier"
	"github.com/santapong/cooker/internal/store"
)

func NotifyCompletedJob(st *store.Store, dispatcher *notifier.Dispatcher) func(context.Context, *jobqueue.Job) error {
	return func(ctx context.Context, job *jobqueue.Job) error {
		if dispatcher == nil {
			return nil
		}
		// Persistent notifications are reconstructed from terminal queue rows.
		// The fast callback is an optimization only; the reconciliation loop and
		// retention barrier cover restart and acceptance-store failures.
		if dispatcher.Outbox != nil {
			return dispatcher.Outbox.ReconcileJobs(ctx, 100)
		}
		id := jobRunID(job)
		if id == "" {
			return nil
		}
		run, err := st.Runs.Get(ctx, id)
		if err != nil {
			return err
		}
		if job.Kind == JobKindAppDeploy {
			var plan AppDeployPlan
			if err := json.Unmarshal(job.Payload, &plan); err != nil {
				return err
			}
			if plan.App != nil {
				NotifyDeployOutcome(dispatcher, plan.App, id, run.Error, run.Status == model.RunStatusSuccess, false)
			}
			return nil
		}
		var payload runJobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return err
		}
		p := payload.Pipeline
		if p == nil {
			p, err = st.Pipelines.Get(ctx, payload.PipelineID)
			if err != nil {
				return err
			}
		}
		NotifyRunOutcome(dispatcher, p, run, nil)
		return nil
	}
}
