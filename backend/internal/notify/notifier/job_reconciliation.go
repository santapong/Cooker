package notifier

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
)

// CaptureTerminalJob is the retention barrier. Its transaction does not alter
// job/run outcomes: an outbox persistence failure prevents deletion only. A
// restart can still reconstruct the event from the retained terminal job.
func (o *Outbox) CaptureTerminalJob(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
	if j == nil || !j.Status.IsTerminal() {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_job_receipts WHERE job_id=$1)`, j.ID).Scan(&exists); err != nil {
		return errors.New("notification receipt lookup unavailable")
	}
	if exists {
		return nil
	}
	disposition := "not_notifiable"
	var eventID any
	var cutover time.Time
	if err := tx.QueryRowContext(ctx, `SELECT cutover_at FROM notification_policy WHERE id='v1'`).Scan(&cutover); err != nil {
		return errors.New("notification policy unavailable")
	}
	finished := j.UpdatedAt
	if j.FinishedAt != nil {
		finished = *j.FinishedAt
	}
	if finished.Before(cutover) {
		disposition = "before_policy_cutover"
	} else if j.Kind == "pipeline-run" || j.Kind == "app-deploy" {
		// Decode only immutable native identities. Arbitrary nested configuration
		// and errors in the queue payload are not persisted in event metadata.
		var p struct {
			RunID      string `json:"runId"`
			PipelineID string `json:"pipelineId"`
			App        *struct {
				ID string `json:"id"`
			} `json:"app"`
		}
		if json.Unmarshal(j.Payload, &p) != nil || p.RunID == "" {
			return errors.New("notification source identity unavailable")
		}
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM pipeline_runs WHERE id=$1`, p.RunID).Scan(&status); err != nil {
			return errors.New("notification source outcome unavailable")
		}
		event := Event{RunID: p.RunID, Timestamp: finished}
		switch status {
		case "success":
			event.Type = EventRunSucceeded
		case "failed":
			event.Type = EventRunFailed
		case "cancelled":
			event.Type = EventRunCancelled
		default:
			return errors.New("notification source outcome is not terminal")
		}
		if j.Kind == "app-deploy" {
			if p.App == nil || p.App.ID == "" {
				return errors.New("notification app identity unavailable")
			}
			event.AppID = p.App.ID
			switch status {
			case "success":
				event.Type = EventDeploySucceeded
			case "failed":
				event.Type = EventDeployFailed
			case "cancelled":
				event.Type = EventDeployCancelled
			}
		} else {
			event.PipelineID = p.PipelineID
		}
		if err := o.acceptTx(ctx, tx, event); err != nil {
			return err
		}
		safe, _ := SafeEvent(event)
		eventID = safe.ID
		disposition = "accepted"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO notification_job_receipts(job_id,event_id,disposition)
	VALUES($1,$2,$3) ON CONFLICT(job_id) DO NOTHING`, j.ID, eventID, disposition)
	if err != nil {
		return errors.New("notification receipt persistence unavailable")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notification_source_repairs WHERE job_id=$1`, j.ID); err != nil {
		return errors.New("notification repair completion unavailable")
	}
	return nil
}

// ReconcileJobs repairs the commit→callback crash window, terminal failures
// (whose old worker path had no callback), cancellation, and operator resolution.
// FOR UPDATE keeps it coordinated with the pre-prune capture transaction.
func (o *Outbox) ReconcileJobs(ctx context.Context, limit int) error {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("notification reconciliation unavailable")
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT j.id,j.kind,j.payload,j.status,j.finished_at,j.updated_at FROM jobs j
	WHERE j.status IN ('succeeded','failed','cancelled') AND NOT EXISTS
	(SELECT 1 FROM notification_job_receipts r WHERE r.job_id=j.id)
	AND NOT EXISTS(SELECT 1 FROM notification_source_repairs r WHERE r.job_id=j.id AND r.next_capture_at>NOW())
	ORDER BY COALESCE(j.finished_at,j.updated_at),j.id LIMIT $1 FOR UPDATE OF j SKIP LOCKED`, limit)
	if err != nil {
		return errors.New("notification reconciliation unavailable")
	}
	var jobs []*jobqueue.Job
	for rows.Next() {
		var j jobqueue.Job
		if err := rows.Scan(&j.ID, &j.Kind, &j.Payload, &j.Status, &j.FinishedAt, &j.UpdatedAt); err != nil {
			rows.Close()
			return errors.New("notification reconciliation unavailable")
		}
		jobs = append(jobs, &j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errors.New("notification reconciliation unavailable")
	}
	partial := false
	for _, j := range jobs {
		if _, err := tx.ExecContext(ctx, `SAVEPOINT notification_capture`); err != nil {
			return errors.New("notification reconciliation unavailable")
		}
		if err := o.CaptureTerminalJob(ctx, tx, j); err != nil {
			partial = true
			if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT notification_capture`); err != nil {
				return errors.New("notification reconciliation unavailable")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO notification_source_repairs(job_id,next_capture_at)
			VALUES($1,NOW()+interval '30 seconds') ON CONFLICT(job_id) DO UPDATE SET
			attempts=LEAST(notification_source_repairs.attempts+1,1000000),
			next_capture_at=NOW()+CASE WHEN notification_source_repairs.attempts=1 THEN interval '2 minutes' ELSE interval '10 minutes' END,
			last_code='source_capture_unavailable',updated_at=NOW()`, j.ID); err != nil {
				return errors.New("notification source repair persistence unavailable")
			}
		}
		if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT notification_capture`); err != nil {
			return errors.New("notification reconciliation unavailable")
		}
	}
	if tx.Commit() != nil {
		return errors.New("notification reconciliation commit unconfirmed")
	}
	if partial {
		return errors.New("some notification sources remain pending; retained for repair")
	}
	return nil
}
