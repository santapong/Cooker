package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func jobRunID(j *jobqueue.Job) string {
	if j.Kind != JobKindAppDeploy && j.Kind != JobKindPipelineRun {
		return ""
	}
	var ref struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(j.Payload, &ref)
	return ref.RunID
}

func HeartbeatJobRun(st *store.Store) func(context.Context, *jobqueue.Job) error {
	return func(ctx context.Context, j *jobqueue.Job) error {
		id := jobRunID(j)
		if id == "" {
			return nil
		}
		// App jobs can recreate their run after a crash immediately following
		// enqueue. The first heartbeat may race that idempotent creation.
		err := st.Runs.UpdateHeartbeat(ctx, id, time.Now().UTC())
		if j.Kind == JobKindAppDeploy && errors.Is(err, store.ErrNotFound) {
			return nil
		}
		// A modern native job also carries the pending view needed by its
		// runner. Its first beat may precede that view's idempotent creation.
		// Tolerate only the verified shape; legacy/malformed jobs and every
		// other store/claim error retain the ordinary heartbeat failure.
		if errors.Is(err, store.ErrNotFound) && ctx.Err() == nil && reconstructibleNativeRun(j) {
			return nil
		}
		return err
	}
}

func RecoverJobRun(st *store.Store) func(context.Context, *jobqueue.Job) error {
	return func(ctx context.Context, j *jobqueue.Job) error {
		id := jobRunID(j)
		if id == "" {
			return nil
		}
		run, err := st.Runs.Get(ctx, id)
		if err != nil {
			return err
		}
		if j.Kind == JobKindAppDeploy {
			failure, decodeErr := store.DecodeAppPreflightFailure(j.Payload)
			if decodeErr == nil {
				reader, ok := st.AppDeploys.(store.AppPreflightFailureReader)
				if !ok {
					return store.ErrConflict
				}
				{
					preserved, readErr := reader.HasAppPreflightFailure(ctx, failure)
					if readErr != nil {
						return readErr
					}
					if preserved {
						return nil
					}
				}
			} else {
				if !store.IsLegacyAppDeploymentPayload(j.Payload) {
					return store.ErrConflict
				}
				guard, ok := st.AppDeploys.(store.AppPreflightLegacyRecoveryGuard)
				if !ok {
					return store.ErrConflict
				}
				{
					if err := guard.CheckAppLegacyRecovery(ctx, id); err != nil {
						return err
					}
				}
			}
		}
		run.Status, run.Error = model.RunStatusNeedsAttention, j.LastError
		run.FinishedAt = nil
		return st.Runs.Update(ctx, run)
	}
}
