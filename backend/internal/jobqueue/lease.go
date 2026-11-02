package jobqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const expiredReason = "worker heartbeat expired; inspect the deployment before recording an outcome"

func (m *Memory) Cancel(_ context.Context, id string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return "", ErrNotFound
	}
	now := time.Now().UTC()
	switch j.Status {
	case StatusPending:
		j.Status, j.FinishedAt, j.UpdatedAt = StatusCancelled, &now, now
	case StatusRunning:
		j.Status, j.LockedBy, j.UpdatedAt = StatusNeedsAttention, "", now
		j.LastError = "cancellation requested; inspect external work before recording an outcome"
	}
	return j.Status, nil
}

func (p *Postgres) Cancel(ctx context.Context, id string) (Status, error) {
	j, err := p.transition(ctx, `UPDATE jobs SET
 status = CASE WHEN status = 'pending' THEN 'cancelled' WHEN status = 'running' THEN 'needs_attention' ELSE status END,
 finished_at = CASE WHEN status = 'pending' THEN NOW() ELSE finished_at END,
 locked_by = CASE WHEN status = 'running' THEN '' ELSE locked_by END,
 last_error = CASE WHEN status = 'running' THEN 'cancellation requested; inspect external work before recording an outcome' ELSE last_error END,
 updated_at = NOW() WHERE id = $1 AND status IN ('pending','running','needs_attention','cancelled')
 RETURNING id, kind, payload, status, last_error`, id)
	if err != nil {
		return "", err
	}
	return j.Status, nil
}

func (m *Memory) Heartbeat(_ context.Context, owner, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	if j.Status != StatusRunning || j.LockedBy != owner {
		return ErrNotLocked
	}
	now := time.Now().UTC()
	j.LockedAt, j.UpdatedAt = &now, now
	return nil
}

func (m *Memory) Suspend(_ context.Context, owner, id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	if j.Status != StatusRunning || j.LockedBy != owner {
		return ErrNotLocked
	}
	j.Status, j.LockedBy, j.LastError, j.UpdatedAt = StatusNeedsAttention, "", reason, time.Now().UTC()
	return nil
}

func (m *Memory) RecoverExpired(_ context.Context, cutoff time.Time) ([]*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var jobs []*Job
	for _, j := range m.jobs {
		if j.Status == StatusRunning && (j.LockedAt == nil || j.LockedAt.Before(cutoff)) {
			j.Status, j.LockedBy, j.LastError, j.UpdatedAt = StatusNeedsAttention, "", expiredReason, time.Now().UTC()
			jobs = append(jobs, cloneJob(j))
		}
	}
	return jobs, nil
}

func validateResolution(outcome Status, evidence string) error {
	if outcome != StatusSucceeded && outcome != StatusFailed && outcome != StatusCancelled {
		return fmt.Errorf("jobqueue: resolution must be succeeded, failed or cancelled")
	}
	if len(strings.TrimSpace(evidence)) < 10 || len(evidence) > 2048 {
		return fmt.Errorf("jobqueue: resolution evidence must contain 10–2048 characters")
	}
	return nil
}

func (m *Memory) Resolve(_ context.Context, id string, outcome Status, evidence string) error {
	if err := validateResolution(outcome, evidence); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	if j.Status != StatusNeedsAttention {
		return ErrNotLocked
	}
	now := time.Now().UTC()
	j.Status, j.LastError, j.UpdatedAt, j.FinishedAt = outcome, evidence, now, &now
	return nil
}

func (p *Postgres) Heartbeat(ctx context.Context, owner, id string) error {
	res, err := p.db.ExecContext(ctx, `UPDATE jobs SET locked_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`, id, owner)
	if err != nil {
		return err
	}
	return checkOwnedRowAffected(res, ctx, p.db, id)
}

func (p *Postgres) Suspend(ctx context.Context, owner, id, reason string) error {
	_, err := p.transition(ctx, `UPDATE jobs SET status = 'needs_attention', locked_by = '', last_error = $3, updated_at = NOW()
 WHERE id = $1 AND locked_by = $2 AND status = 'running' RETURNING id, kind, payload, status, last_error`, id, owner, reason)
	return err
}

func (p *Postgres) RecoverExpired(ctx context.Context, cutoff time.Time) ([]*Job, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // Commit/operation errors determine the result.
	rows, err := tx.QueryContext(ctx, `UPDATE jobs SET status = 'needs_attention', locked_by = '', last_error = $2, updated_at = NOW()
 WHERE status = 'running' AND (locked_at IS NULL OR locked_at < $1) RETURNING id, kind, payload, status, last_error`, cutoff, expiredReason)
	if err != nil {
		return nil, err
	}
	var jobs []*Job
	for rows.Next() {
		j := &Job{}
		if err := rows.Scan(&j.ID, &j.Kind, &j.Payload, &j.Status, &j.LastError); err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if p.OnTransition != nil {
			if err := p.OnTransition(ctx, tx, j); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (p *Postgres) Resolve(ctx context.Context, id string, outcome Status, evidence string) error {
	if err := validateResolution(outcome, evidence); err != nil {
		return err
	}
	_, err := p.transition(ctx, `UPDATE jobs SET status = $2, last_error = $3, finished_at = NOW(), updated_at = NOW()
 WHERE id = $1 AND status = 'needs_attention' RETURNING id, kind, payload, status, last_error`, id, outcome, evidence)
	return err
}

func (p *Postgres) transition(ctx context.Context, query string, args ...any) (*Job, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // Commit/operation errors determine the result.
	j := &Job{}
	err = tx.QueryRowContext(ctx, query, args...).Scan(&j.ID, &j.Kind, &j.Payload, &j.Status, &j.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotLocked
	}
	if err != nil {
		return nil, err
	}
	if p.OnTransition != nil {
		if err := p.OnTransition(ctx, tx, j); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return j, nil
}
