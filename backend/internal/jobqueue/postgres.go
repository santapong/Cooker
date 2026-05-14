package jobqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Postgres is a Store backed by the jobs table created in migration
// 010. Pickup uses FOR UPDATE SKIP LOCKED so N workers can call
// Dequeue concurrently without coordination.
type Postgres struct {
	db *sql.DB
}

// NewPostgres wraps an existing *sql.DB. The caller owns the
// connection pool lifecycle; this type does not close it.
func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db} }

// Enqueue implements Store.
func (p *Postgres) Enqueue(ctx context.Context, kind string, payload json.RawMessage, opts EnqueueOptions) (*Job, error) {
	if kind == "" {
		return nil, fmt.Errorf("jobqueue: empty kind")
	}
	now := time.Now().UTC()
	runAt := opts.RunAt
	if runAt.IsZero() {
		runAt = now
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	id := opts.ID
	if id == "" {
		id = newID()
	}
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	j := &Job{
		ID:             id,
		Kind:           kind,
		Payload:        payload,
		Status:         StatusPending,
		MaxAttempts:    maxAttempts,
		RunAt:          runAt,
		ConcurrencyKey: opts.ConcurrencyKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO jobs (id, kind, payload, status, max_attempts, run_at,
		                  concurrency_key, created_at, updated_at)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $7)
	`, j.ID, j.Kind, []byte(j.Payload), j.MaxAttempts, j.RunAt, j.ConcurrencyKey, j.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("jobqueue: enqueue: %w", err)
	}
	return j, nil
}

// Dequeue implements Store. The query:
//
//	1. Picks the oldest pending job whose run_at <= NOW().
//	2. Filters by kind when the caller supplied a non-empty list.
//	3. Skips jobs whose concurrency_key is already held by a running
//	   job (NOT EXISTS subquery; partial index keeps it cheap).
//	4. Locks the row with FOR UPDATE SKIP LOCKED so concurrent workers
//	   don't contend.
//	5. UPDATE ... RETURNING flips it to running and returns the row.
func (p *Postgres) Dequeue(ctx context.Context, workerID string, kinds []string) (*Job, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("jobqueue: begin dequeue tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The empty-kinds case is handled by the array-overlap operator
	// short-circuit ($1::text[] = '{}'). We pass a Postgres text[]
	// literal so a nil/empty slice doesn't require a different query.
	kindsArr := pgTextArray(kinds)

	var (
		id             string
		kind           string
		payload        []byte
		status         string
		attempts       int
		maxAttempts    int
		runAt          time.Time
		lockedBy       string
		lockedAt       sql.NullTime
		startedAt      sql.NullTime
		finishedAt     sql.NullTime
		lastError      string
		concurrencyKey string
		createdAt      time.Time
		updatedAt      time.Time
	)
	row := tx.QueryRowContext(ctx, `
		WITH picked AS (
			SELECT j.id
			  FROM jobs j
			 WHERE j.status = 'pending'
			   AND j.run_at <= NOW()
			   AND ($1::text[] = '{}' OR j.kind = ANY($1::text[]))
			   AND (
			       j.concurrency_key = ''
			       OR NOT EXISTS (
			           SELECT 1 FROM jobs j2
			            WHERE j2.concurrency_key = j.concurrency_key
			              AND j2.status = 'running'
			       )
			   )
			 ORDER BY j.run_at
			 LIMIT 1
			 FOR UPDATE SKIP LOCKED
		)
		UPDATE jobs
		   SET status = 'running',
		       locked_by = $2,
		       locked_at = NOW(),
		       started_at = NOW(),
		       attempts = attempts + 1,
		       updated_at = NOW()
		 WHERE id = (SELECT id FROM picked)
		 RETURNING id, kind, payload, status, attempts, max_attempts, run_at,
		           locked_by, locked_at, started_at, finished_at, last_error,
		           concurrency_key, created_at, updated_at
	`, kindsArr, workerID)
	err = row.Scan(&id, &kind, &payload, &status, &attempts, &maxAttempts, &runAt,
		&lockedBy, &lockedAt, &startedAt, &finishedAt, &lastError,
		&concurrencyKey, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// No work available — caller's responsibility to sleep / wait.
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, fmt.Errorf("jobqueue: dequeue scan: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("jobqueue: dequeue commit: %w", err)
	}
	j := &Job{
		ID:             id,
		Kind:           kind,
		Payload:        json.RawMessage(payload),
		Status:         Status(status),
		Attempts:       attempts,
		MaxAttempts:    maxAttempts,
		RunAt:          runAt,
		LockedBy:       lockedBy,
		LastError:      lastError,
		ConcurrencyKey: concurrencyKey,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
	if lockedAt.Valid {
		t := lockedAt.Time
		j.LockedAt = &t
	}
	if startedAt.Valid {
		t := startedAt.Time
		j.StartedAt = &t
	}
	if finishedAt.Valid {
		t := finishedAt.Time
		j.FinishedAt = &t
	}
	return j, nil
}

func (p *Postgres) Complete(ctx context.Context, workerID, jobID string) error {
	res, err := p.db.ExecContext(ctx, `
		UPDATE jobs
		   SET status = 'succeeded',
		       finished_at = NOW(),
		       locked_by = '',
		       updated_at = NOW()
		 WHERE id = $1 AND locked_by = $2 AND status = 'running'
	`, jobID, workerID)
	if err != nil {
		return fmt.Errorf("jobqueue: complete: %w", err)
	}
	return checkOwnedRowAffected(res, ctx, p.db, jobID)
}

func (p *Postgres) Fail(ctx context.Context, workerID, jobID string, lastError string) error {
	res, err := p.db.ExecContext(ctx, `
		UPDATE jobs
		   SET status = 'failed',
		       finished_at = NOW(),
		       last_error = $3,
		       locked_by = '',
		       updated_at = NOW()
		 WHERE id = $1 AND locked_by = $2 AND status = 'running'
	`, jobID, workerID, lastError)
	if err != nil {
		return fmt.Errorf("jobqueue: fail: %w", err)
	}
	return checkOwnedRowAffected(res, ctx, p.db, jobID)
}

func (p *Postgres) Reschedule(ctx context.Context, workerID, jobID string, nextRunAt time.Time, lastError string) error {
	// Atomically branch: if attempts >= max_attempts the row
	// transitions to 'failed' (terminal); otherwise it returns to
	// 'pending' with run_at bumped. Doing this in a single UPDATE
	// avoids a read-modify-write race against another worker that
	// might pick up the row in between.
	res, err := p.db.ExecContext(ctx, `
		UPDATE jobs
		   SET status = CASE WHEN attempts >= max_attempts
		                     THEN 'failed' ELSE 'pending' END,
		       run_at = CASE WHEN attempts >= max_attempts
		                     THEN run_at ELSE $3 END,
		       finished_at = CASE WHEN attempts >= max_attempts
		                          THEN NOW() ELSE NULL END,
		       last_error = $4,
		       locked_by = '',
		       locked_at = NULL,
		       started_at = NULL,
		       updated_at = NOW()
		 WHERE id = $1 AND locked_by = $2 AND status = 'running'
	`, jobID, workerID, nextRunAt, lastError)
	if err != nil {
		return fmt.Errorf("jobqueue: reschedule: %w", err)
	}
	return checkOwnedRowAffected(res, ctx, p.db, jobID)
}

func (p *Postgres) Get(ctx context.Context, jobID string) (*Job, error) {
	var (
		j                                       Job
		payload                                 []byte
		status                                  string
		lockedAt, startedAt, finishedAt         sql.NullTime
	)
	row := p.db.QueryRowContext(ctx, `
		SELECT id, kind, payload, status, attempts, max_attempts, run_at,
		       locked_by, locked_at, started_at, finished_at, last_error,
		       concurrency_key, created_at, updated_at
		  FROM jobs WHERE id = $1
	`, jobID)
	err := row.Scan(&j.ID, &j.Kind, &payload, &status, &j.Attempts, &j.MaxAttempts, &j.RunAt,
		&j.LockedBy, &lockedAt, &startedAt, &finishedAt, &j.LastError,
		&j.ConcurrencyKey, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("jobqueue: get: %w", err)
	}
	j.Payload = json.RawMessage(payload)
	j.Status = Status(status)
	if lockedAt.Valid {
		t := lockedAt.Time
		j.LockedAt = &t
	}
	if startedAt.Valid {
		t := startedAt.Time
		j.StartedAt = &t
	}
	if finishedAt.Valid {
		t := finishedAt.Time
		j.FinishedAt = &t
	}
	return &j, nil
}

func (p *Postgres) Stats(ctx context.Context) (map[Status]int, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("jobqueue: stats: %w", err)
	}
	defer rows.Close()
	out := make(map[Status]int)
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[Status(s)] = n
	}
	return out, rows.Err()
}

// checkOwnedRowAffected disambiguates a zero-rows-affected result.
// Either the job doesn't exist at all (ErrNotFound) or it exists but
// the caller doesn't own the lock (ErrNotLocked). Worth the extra
// roundtrip so the worker loop logs a useful error instead of a
// generic "no rows updated".
func checkOwnedRowAffected(res sql.Result, ctx context.Context, db *sql.DB, jobID string) error {
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}
	var status string
	var lockedBy string
	err := db.QueryRowContext(ctx, `SELECT status, locked_by FROM jobs WHERE id = $1`, jobID).
		Scan(&status, &lockedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("jobqueue: post-update check: %w", err)
	}
	return ErrNotLocked
}

// pgTextArray formats a []string as a Postgres text[] literal.
// Empty input yields '{}'. Special characters are escaped per
// Postgres array-literal rules so we don't need a driver-specific
// array type (lib/pq has one, but keeping it dep-free here makes
// the package easier to test in isolation).
func pgTextArray(ss []string) string {
	if len(ss) == 0 {
		return "{}"
	}
	var b []byte
	b = append(b, '{')
	for i, s := range ss {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '"')
		for _, c := range s {
			if c == '"' || c == '\\' {
				b = append(b, '\\')
			}
			b = append(b, byte(c))
		}
		b = append(b, '"')
	}
	b = append(b, '}')
	return string(b)
}
