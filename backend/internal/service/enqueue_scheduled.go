package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

// ScheduledOccurrence is server-derived admission input, never HTTP authority.
// ScheduledFor is the due slot read from PostgreSQL, rather than the time a
// worker happened to wake up. PostgreSQL timestamps have microsecond precision.
type ScheduledOccurrence struct {
	ScheduleID      string    `json:"scheduleId"`
	ScheduleVersion int       `json:"scheduleVersion"`
	ScheduledFor    time.Time `json:"scheduledFor"`
	PipelineID      string    `json:"pipelineId"`
	CronExpr        string    `json:"cronExpr"`
	Timezone        string    `json:"timezone"`
	AdmittedAt      time.Time `json:"admittedAt"`
	NextRunAt       time.Time `json:"nextRunAt"`
}

// RunID identifies the occurrence independently of retries, replicas, edits,
// or delayed ticks. Other pins are checked at admission, not folded into a new
// ID that could turn an edited version of the same slot into a second run.
func (o ScheduledOccurrence) RunID() string {
	stamp := o.ScheduledFor.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	hash := sha256.Sum256([]byte(o.ScheduleID + "\x00" + stamp))
	return "sched_" + hex.EncodeToString(hash[:])
}

// EnqueueScheduledRun retains the same immutable pipeline/run snapshot and
// concurrency key as native manual admission. The production queue's atomic
// OnEnqueue chain verifies these pins, journals the occurrence and advances
// the schedule before committing the pending job. No MarkFired call follows.
func (e *JobQueueEnqueuer) EnqueueScheduledRun(ctx context.Context, occurrence ScheduledOccurrence) error {
	if e == nil || e.store == nil || e.pipelines == nil {
		return fmt.Errorf("enqueuer: scheduled work requires a durable queue and pipeline snapshots")
	}
	atomic, ok := e.store.(interface{ AtomicTriggerAdmission() bool })
	if !ok || !atomic.AtomicTriggerAdmission() {
		return fmt.Errorf("enqueuer: scheduled work requires atomic admission")
	}
	if occurrence.ScheduleID == "" || occurrence.ScheduleVersion < 1 || occurrence.PipelineID == "" || occurrence.ScheduledFor.IsZero() || occurrence.AdmittedAt.IsZero() || !occurrence.NextRunAt.After(occurrence.AdmittedAt) {
		return fmt.Errorf("enqueuer: invalid scheduled occurrence")
	}
	if e.AdmissionGuard != nil {
		return e.AdmissionGuard.WithNativeTriggerAdmission(ctx, "scheduled:"+occurrence.RunID(), func(ctx context.Context) error {
			return e.enqueueScheduledOccurrence(ctx, occurrence)
		})
	}
	return e.enqueueScheduledOccurrence(ctx, occurrence)
}

func (e *JobQueueEnqueuer) existingScheduledOccurrence(ctx context.Context, occurrence ScheduledOccurrence) (bool, error) {
	j, err := e.store.Get(ctx, "run:"+occurrence.RunID())
	if errors.Is(err, jobqueue.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("accepted scheduled receipt is unavailable")
	}
	var plan struct {
		runJobPayload
		Occurrence ScheduledOccurrence `json:"scheduledOccurrence"`
	}
	if json.Unmarshal(j.Payload, &plan) != nil || j.Kind != JobKindPipelineRun || plan.RunID != occurrence.RunID() || plan.PipelineID != occurrence.PipelineID || plan.Occurrence.ScheduleID != occurrence.ScheduleID || !plan.Occurrence.ScheduledFor.UTC().Truncate(time.Microsecond).Equal(occurrence.ScheduledFor.UTC().Truncate(time.Microsecond)) || plan.Pipeline == nil || plan.Run == nil || plan.Run.ID != plan.RunID || plan.Run.PipelineID != plan.PipelineID {
		return true, fmt.Errorf("scheduled occurrence conflicts with an accepted job")
	}
	if plan.Registry != nil && validateNativeRegistryPlan(plan.Pipeline, plan.Registry) != nil {
		return true, model.ErrInvalidRegistryAuthority
	}
	if e.RequireRegistryPins && plan.Registry == nil {
		return true, ErrRegistryAuthorityRequired
	}
	return true, nil
}

func (e *JobQueueEnqueuer) enqueueScheduledOccurrence(ctx context.Context, occurrence ScheduledOccurrence) error {
	if found, err := e.existingScheduledOccurrence(ctx, occurrence); found || err != nil {
		return err
	}
	snapshot, err := e.pipelines.Get(ctx, occurrence.PipelineID)
	if err != nil {
		return fmt.Errorf("enqueuer: snapshot scheduled pipeline: %w", err)
	}
	if (snapshot.Source != nil || e.PrepareRegistry != nil || e.RequireRegistryPins) && e.AdmissionGuard == nil {
		return fmt.Errorf("native admission serialization is unavailable")
	}
	runID := occurrence.RunID()
	plan := struct {
		runJobPayload
		Occurrence ScheduledOccurrence `json:"scheduledOccurrence"`
	}{runJobPayload: runJobPayload{PipelineID: occurrence.PipelineID, RunID: runID, Pipeline: snapshot, Run: pendingPipelineRun(snapshot, runID)}, Occurrence: occurrence}
	if err := e.prepareRegistryPlan(ctx, &plan.runJobPayload); err != nil {
		return err
	}
	if err := e.prepareSourcePlan(ctx, &plan.runJobPayload); err != nil {
		return err
	}
	if err := e.prepareSecretPlan(ctx, &plan.runJobPayload); err != nil {
		return err
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("enqueuer: marshal scheduled payload: %w", err)
	}
	_, err = e.store.Enqueue(ctx, JobKindPipelineRun, payload, jobqueue.EnqueueOptions{
		ID: "run:" + runID, MaxAttempts: 1, ConcurrencyKey: "pipeline:" + occurrence.PipelineID,
	})
	if err != nil {
		if found, readErr := e.existingScheduledOccurrence(ctx, occurrence); found || readErr != nil {
			return readErr
		}
	}
	return err
}
