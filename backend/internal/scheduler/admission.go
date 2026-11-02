package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

// SyncOccurrenceAdmission is part of the queue's production OnEnqueue chain.
// The pending job INSERT, occurrence journal and schedule advancement commit
// together. An error, process loss or cancelled transaction leaves none of
// them accepted. The advisory leader lock remains an optimization only.
func SyncOccurrenceAdmission(ctx context.Context, tx *sql.Tx, job *jobqueue.Job) error {
	if job == nil || job.Kind != service.JobKindPipelineRun {
		return nil
	}
	var input struct {
		PipelineID string                       `json:"pipelineId"`
		RunID      string                       `json:"runId"`
		Pipeline   *model.Pipeline              `json:"pipeline"`
		Run        *model.PipelineRun           `json:"run"`
		Occurrence *service.ScheduledOccurrence `json:"scheduledOccurrence"`
	}
	if err := json.Unmarshal(job.Payload, &input); err != nil {
		return fmt.Errorf("scheduler: invalid queue admission payload")
	}
	if input.Occurrence == nil {
		return nil // Existing native/manual pipeline admission is unchanged.
	}
	o := *input.Occurrence
	if o.ScheduleID == "" || o.ScheduleVersion < 1 || o.ScheduledFor.IsZero() || o.AdmittedAt.IsZero() || o.ScheduledFor.After(o.AdmittedAt) || !o.NextRunAt.After(o.AdmittedAt) || input.PipelineID != o.PipelineID || input.RunID != o.RunID() || job.ID != "run:"+input.RunID || job.ConcurrencyKey != "pipeline:"+o.PipelineID || job.MaxAttempts != 1 || input.Pipeline == nil || input.Pipeline.ID != o.PipelineID || input.Run == nil || input.Run.ID != input.RunID || input.Run.PipelineID != o.PipelineID || input.Run.PipelineVersion != input.Pipeline.Version || input.Run.Status != model.RunStatusPending {
		return fmt.Errorf("scheduler: scheduled job identity or immutable plan changed: %w", ErrConflict)
	}
	current, err := scanSchedule(tx.QueryRowContext(ctx, `
		SELECT id, version, pipeline_id, name, cron_expr, timezone, last_run_at,
		       last_run_id, next_run_at, enabled, created_at, updated_at
		  FROM schedules WHERE id=$1 FOR UPDATE`, o.ScheduleID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("scheduler: lock occurrence: %w", err)
	}
	if !current.Enabled || current.Version != o.ScheduleVersion || !current.NextRunAt.Equal(o.ScheduledFor) || current.PipelineID != o.PipelineID || current.CronExpr != o.CronExpr || current.Timezone != o.Timezone {
		return ErrConflict
	}
	cron, err := Parse(current.CronExpr)
	if err != nil {
		return fmt.Errorf("scheduler: stored cron is invalid")
	}
	next, err := cron.Next(o.AdmittedAt, current.LoadLocation())
	if err != nil || !next.Equal(o.NextRunAt) {
		return fmt.Errorf("scheduler: next occurrence changed: %w", ErrConflict)
	}
	// Do not delete this journal with jobs or schedules. A retained occurrence
	// prevents replay even after terminal-job retention or ID reuse.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO schedule_occurrences
		 (schedule_id, scheduled_for, schedule_version, pipeline_id, cron_expr,
		  timezone, run_id, job_id, admitted_at, next_run_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		o.ScheduleID, o.ScheduledFor.UTC().Truncate(time.Microsecond), o.ScheduleVersion,
		o.PipelineID, o.CronExpr, o.Timezone, input.RunID, job.ID, o.AdmittedAt, o.NextRunAt); err != nil {
		return fmt.Errorf("scheduler: record occurrence: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE schedules SET last_run_at=$2, last_run_id=$3, next_run_at=$4,
		 version=version+1, updated_at=NOW()
		 WHERE id=$1 AND version=$5 AND next_run_at=$6 AND enabled=TRUE`,
		o.ScheduleID, o.AdmittedAt, input.RunID, o.NextRunAt, o.ScheduleVersion, o.ScheduledFor)
	if err != nil {
		return fmt.Errorf("scheduler: advance occurrence: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return ErrConflict
	}
	return nil
}
