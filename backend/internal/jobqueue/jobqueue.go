// Package jobqueue provides a durable async job queue so pipeline-run
// execution can be decoupled from the HTTP request that triggered it.
//
// A producer calls Enqueue with a Kind and a JSON payload; one of N
// worker goroutines (wired in slice 2) calls Dequeue, runs the job's
// registered handler, and reports the outcome via Complete / Fail /
// Reschedule. The Postgres implementation uses FOR UPDATE SKIP LOCKED
// for lock-free pickup; the in-memory implementation mirrors the same
// semantics so handler tests don't need a database.
//
// Concurrency-key semantics: a non-empty ConcurrencyKey enforces that
// at most one job with that key is in 'running' at any time. The check
// is performed inside Dequeue so a contending job stays 'pending' and
// is re-evaluated on the next worker tick, rather than being skipped
// permanently.
//
// Slice 1 (this file): types, Store interface, sentinel errors,
// Memory + Postgres stores, unit tests. No worker loop, no
// LISTEN/NOTIFY, no handler wiring — those land in slice 2.
package jobqueue

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Status is the lifecycle state of a Job.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// IsTerminal reports whether the status is a final state.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Job is a unit of work persisted in the queue. Fields mirror the
// jobs table columns.
type Job struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"payload"`
	Status         Status          `json:"status"`
	Attempts       int             `json:"attempts"`
	MaxAttempts    int             `json:"maxAttempts"`
	RunAt          time.Time       `json:"runAt"`
	LockedBy       string          `json:"lockedBy,omitempty"`
	LockedAt       *time.Time      `json:"lockedAt,omitempty"`
	StartedAt      *time.Time      `json:"startedAt,omitempty"`
	FinishedAt     *time.Time      `json:"finishedAt,omitempty"`
	LastError      string          `json:"lastError,omitempty"`
	ConcurrencyKey string          `json:"concurrencyKey,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// EnqueueOptions configures a single Enqueue call. Zero-valued fields
// fall back to sensible defaults documented per field.
type EnqueueOptions struct {
	// RunAt schedules the job to be invisible to Dequeue until this
	// timestamp. Zero means "runnable immediately".
	RunAt time.Time
	// MaxAttempts caps the total run attempts. Reschedule increments
	// Attempts; once Attempts == MaxAttempts, Reschedule transitions
	// the job to StatusFailed instead of returning to pending.
	// Zero defaults to 1 (no retry).
	MaxAttempts int
	// ConcurrencyKey, when non-empty, enforces at most one running
	// job per key. Useful for serialising deploys of the same
	// pipeline / app without a global lock.
	ConcurrencyKey string
	// ID overrides the auto-generated job ID. Mostly for tests and
	// idempotent re-enqueue scenarios. Empty = generated.
	ID string
}

// Sentinel errors.
var (
	// ErrNotFound is returned when a job ID does not exist.
	ErrNotFound = errors.New("jobqueue: job not found")
	// ErrNotLocked is returned when Complete/Fail/Reschedule is
	// called on a job that the caller does not currently own.
	// Defensive: catches double-handling bugs in the worker loop.
	ErrNotLocked = errors.New("jobqueue: job not locked by caller")
)

// Store is the queue interface. Two implementations: postgres (the
// production backend) and memory (tests + memory-only fallback).
type Store interface {
	// Enqueue inserts a new pending job. Returns the persisted Job
	// with ID, timestamps, and defaults filled in.
	Enqueue(ctx context.Context, kind string, payload json.RawMessage, opts EnqueueOptions) (*Job, error)

	// Dequeue attempts to claim the next runnable job whose Kind is
	// in the supplied filter (empty filter = any kind). On success
	// returns the claimed job with Status=running, LockedBy=workerID,
	// LockedAt=now, StartedAt=now, Attempts incremented by 1.
	//
	// (nil, nil) signals "no work available right now" — not an
	// error. Workers should sleep / wait-for-NOTIFY and retry.
	Dequeue(ctx context.Context, workerID string, kinds []string) (*Job, error)

	// Complete marks a locked job as succeeded.
	Complete(ctx context.Context, workerID, jobID string) error

	// Fail marks a locked job as failed (terminal). lastError is
	// persisted on the row for surfacing to the UI / audit log.
	Fail(ctx context.Context, workerID, jobID string, lastError string) error

	// Reschedule returns a locked job to pending state with
	// run_at=nextRunAt and attempts incremented. If the prior
	// attempts already hit MaxAttempts, the job transitions to
	// StatusFailed instead (callers should treat this as terminal).
	Reschedule(ctx context.Context, workerID, jobID string, nextRunAt time.Time, lastError string) error

	// Get retrieves a job by ID. Returns ErrNotFound if missing.
	Get(ctx context.Context, jobID string) (*Job, error)

	// Stats returns counts per status. Cheap enough for the
	// /metrics scraper to call on a 15s interval.
	Stats(ctx context.Context) (map[Status]int, error)

	// DeleteOlderThan removes TERMINAL jobs (succeeded / failed /
	// cancelled) whose finish time is before cutoff, returning the
	// count. Pending and running rows are never touched — retention
	// must not eat queued or in-flight work. Backs the daily
	// COOKER_JOBQUEUE_RETENTION sweep.
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}

// newID returns a job ID. Format: "job_<unix-nano>_<rand>".
// We don't pull in google/uuid here because the other stores already
// use opaque text PKs and this keeps the package dep-free.
var newID = func() string {
	// time.Now().UnixNano() is monotonic per process; jittered with
	// a randomish suffix produced via the package's idCounter so
	// rapid back-to-back Enqueues don't collide.
	n := nextIDSeq()
	return formatID(time.Now().UnixNano(), n)
}
