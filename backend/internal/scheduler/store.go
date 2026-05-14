package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNotFound is returned by Get when no schedule matches the ID.
var ErrNotFound = errors.New("scheduler: schedule not found")

// Store persists schedules. Both implementations (Postgres + Memory)
// must implement DueBefore so the scheduler tick can fetch the ready
// set without scanning the full table on every wakeup.
type Store interface {
	List(ctx context.Context) ([]Schedule, error)
	Get(ctx context.Context, id string) (Schedule, error)
	Create(ctx context.Context, s Schedule) error
	Update(ctx context.Context, s Schedule) error
	Delete(ctx context.Context, id string) error
	// DueBefore returns enabled schedules with next_run_at <= cutoff.
	// Ordered by next_run_at ascending so the caller fires the
	// most-overdue schedules first.
	DueBefore(ctx context.Context, cutoff time.Time) ([]Schedule, error)
	// MarkFired persists the post-fire state in one round-trip:
	// last_run_at = firedAt, last_run_id = runID, next_run_at =
	// nextRunAt. Done as a single UPDATE so a concurrent reader
	// observes consistent state.
	MarkFired(ctx context.Context, id string, firedAt time.Time, runID string, nextRunAt time.Time) error
}

// MemoryStore is an in-process Store used by tests.
type MemoryStore struct {
	mu        sync.RWMutex
	schedules map[string]Schedule
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{schedules: make(map[string]Schedule)}
}

func (m *MemoryStore) List(_ context.Context) ([]Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Schedule, 0, len(m.schedules))
	for _, s := range m.schedules {
		out = append(out, s)
	}
	return out, nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.schedules[id]
	if !ok {
		return Schedule{}, ErrNotFound
	}
	return s, nil
}

func (m *MemoryStore) Create(_ context.Context, s Schedule) error {
	if s.ID == "" {
		return fmt.Errorf("scheduler: Create with empty ID")
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.schedules[s.ID]; exists {
		return fmt.Errorf("scheduler: duplicate schedule id %s", s.ID)
	}
	m.schedules[s.ID] = s
	return nil
}

func (m *MemoryStore) Update(_ context.Context, s Schedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.schedules[s.ID]; !ok {
		return ErrNotFound
	}
	s.UpdatedAt = time.Now().UTC()
	m.schedules[s.ID] = s
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.schedules[id]; !ok {
		return ErrNotFound
	}
	delete(m.schedules, id)
	return nil
}

func (m *MemoryStore) DueBefore(_ context.Context, cutoff time.Time) ([]Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Schedule
	for _, s := range m.schedules {
		if s.Enabled && !s.NextRunAt.After(cutoff) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *MemoryStore) MarkFired(_ context.Context, id string, firedAt time.Time, runID string, nextRunAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.schedules[id]
	if !ok {
		return ErrNotFound
	}
	ft := firedAt
	s.LastRunAt = &ft
	s.LastRunID = runID
	s.NextRunAt = nextRunAt
	s.UpdatedAt = time.Now().UTC()
	m.schedules[id] = s
	return nil
}

// PostgresStore is the production Store.
type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore { return &PostgresStore{db: db} }

func (p *PostgresStore) List(ctx context.Context) ([]Schedule, error) {
	return p.query(ctx, "ORDER BY created_at")
}

func (p *PostgresStore) query(ctx context.Context, suffix string) ([]Schedule, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, pipeline_id, name, cron_expr, timezone, last_run_at,
		       last_run_id, next_run_at, enabled, created_at, updated_at
		  FROM schedules `+suffix)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list: %w", err)
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *PostgresStore) Get(ctx context.Context, id string) (Schedule, error) {
	row := p.db.QueryRowContext(ctx, `
		SELECT id, pipeline_id, name, cron_expr, timezone, last_run_at,
		       last_run_id, next_run_at, enabled, created_at, updated_at
		  FROM schedules WHERE id = $1`, id)
	s, err := scanSchedule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	return s, err
}

func (p *PostgresStore) Create(ctx context.Context, s Schedule) error {
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO schedules
		  (id, pipeline_id, name, cron_expr, timezone, next_run_at,
		   enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		s.ID, s.PipelineID, s.Name, s.CronExpr, s.Timezone, s.NextRunAt,
		s.Enabled, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("scheduler: create: %w", err)
	}
	return nil
}

func (p *PostgresStore) Update(ctx context.Context, s Schedule) error {
	res, err := p.db.ExecContext(ctx, `
		UPDATE schedules
		   SET pipeline_id=$2, name=$3, cron_expr=$4, timezone=$5,
		       next_run_at=$6, enabled=$7, updated_at=NOW()
		 WHERE id=$1`,
		s.ID, s.PipelineID, s.Name, s.CronExpr, s.Timezone, s.NextRunAt, s.Enabled)
	if err != nil {
		return fmt.Errorf("scheduler: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresStore) Delete(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM schedules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("scheduler: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresStore) DueBefore(ctx context.Context, cutoff time.Time) ([]Schedule, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, pipeline_id, name, cron_expr, timezone, last_run_at,
		       last_run_id, next_run_at, enabled, created_at, updated_at
		  FROM schedules
		 WHERE enabled = TRUE AND next_run_at <= $1
		 ORDER BY next_run_at`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("scheduler: due-before: %w", err)
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *PostgresStore) MarkFired(ctx context.Context, id string, firedAt time.Time, runID string, nextRunAt time.Time) error {
	res, err := p.db.ExecContext(ctx, `
		UPDATE schedules
		   SET last_run_at = $2,
		       last_run_id = $3,
		       next_run_at = $4,
		       updated_at = NOW()
		 WHERE id = $1`,
		id, firedAt, runID, nextRunAt)
	if err != nil {
		return fmt.Errorf("scheduler: mark-fired: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanSchedule(row scannable) (Schedule, error) {
	var (
		s         Schedule
		lastRunAt sql.NullTime
		name      sql.NullString
		lastRunID sql.NullString
	)
	if err := row.Scan(&s.ID, &s.PipelineID, &name, &s.CronExpr, &s.Timezone,
		&lastRunAt, &lastRunID, &s.NextRunAt, &s.Enabled,
		&s.CreatedAt, &s.UpdatedAt); err != nil {
		return Schedule{}, err
	}
	if name.Valid {
		s.Name = name.String
	}
	if lastRunID.Valid {
		s.LastRunID = lastRunID.String
	}
	if lastRunAt.Valid {
		t := lastRunAt.Time
		s.LastRunAt = &t
	}
	return s, nil
}
