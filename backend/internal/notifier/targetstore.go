package notifier

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lib/pq"
)

// ErrTargetNotFound is returned by Get when the ID is unknown.
var ErrTargetNotFound = errors.New("notifier: target not found")

// TargetStore is the persistence interface for notification
// targets. The dispatcher only needs ListEnabled; the other methods
// exist for the future settings UI / API.
type TargetStore interface {
	ListEnabled(ctx context.Context) ([]Target, error)
	List(ctx context.Context) ([]Target, error)
	Get(ctx context.Context, id string) (Target, error)
	Create(ctx context.Context, t Target) error
	Update(ctx context.Context, t Target) error
	Delete(ctx context.Context, id string) error
}

// MemoryTargetStore is a process-local store for tests and as a
// fallback when no DB is configured.
type MemoryTargetStore struct {
	mu      sync.RWMutex
	targets map[string]Target
}

// NewMemoryTargetStore returns an empty in-memory TargetStore.
func NewMemoryTargetStore() *MemoryTargetStore {
	return &MemoryTargetStore{targets: make(map[string]Target)}
}

func (m *MemoryTargetStore) ListEnabled(_ context.Context) ([]Target, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Target, 0, len(m.targets))
	for _, t := range m.targets {
		if t.Enabled {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *MemoryTargetStore) List(_ context.Context) ([]Target, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Target, 0, len(m.targets))
	for _, t := range m.targets {
		out = append(out, t)
	}
	return out, nil
}

func (m *MemoryTargetStore) Get(_ context.Context, id string) (Target, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.targets[id]
	if !ok {
		return Target{}, ErrTargetNotFound
	}
	return t, nil
}

func (m *MemoryTargetStore) Create(_ context.Context, t Target) error {
	if t.ID == "" {
		return fmt.Errorf("notifier: Create with empty ID")
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.targets[t.ID]; exists {
		return fmt.Errorf("notifier: duplicate target id %s", t.ID)
	}
	m.targets[t.ID] = t
	return nil
}

func (m *MemoryTargetStore) Update(_ context.Context, t Target) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.targets[t.ID]; !ok {
		return ErrTargetNotFound
	}
	t.UpdatedAt = time.Now().UTC()
	m.targets[t.ID] = t
	return nil
}

func (m *MemoryTargetStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.targets[id]; !ok {
		return ErrTargetNotFound
	}
	delete(m.targets, id)
	return nil
}

// PostgresTargetStore is the production TargetStore backed by the
// notification_targets table (migration 011).
type PostgresTargetStore struct {
	db *sql.DB
}

// NewPostgresTargetStore wraps an existing *sql.DB.
func NewPostgresTargetStore(db *sql.DB) *PostgresTargetStore {
	return &PostgresTargetStore{db: db}
}

func (p *PostgresTargetStore) ListEnabled(ctx context.Context) ([]Target, error) {
	return p.query(ctx, "WHERE enabled = TRUE ORDER BY created_at")
}

func (p *PostgresTargetStore) List(ctx context.Context) ([]Target, error) {
	return p.query(ctx, "ORDER BY created_at")
}

func (p *PostgresTargetStore) query(ctx context.Context, suffix string) ([]Target, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, name, kind, config, event_types, enabled, created_at, updated_at
		  FROM notification_targets `+suffix)
	if err != nil {
		return nil, fmt.Errorf("notifier: list targets: %w", err)
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *PostgresTargetStore) Get(ctx context.Context, id string) (Target, error) {
	row := p.db.QueryRowContext(ctx, `
		SELECT id, name, kind, config, event_types, enabled, created_at, updated_at
		  FROM notification_targets WHERE id = $1`, id)
	t, err := scanTarget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, ErrTargetNotFound
	}
	return t, err
}

func (p *PostgresTargetStore) Create(ctx context.Context, t Target) error {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	cfg := []byte(t.Config)
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	eventTypes := eventTypeArray(t.EventTypes)
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO notification_targets
		  (id, name, kind, config, event_types, enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.ID, t.Name, t.Kind, cfg, pq.Array(eventTypes), t.Enabled, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("notifier: create target: %w", err)
	}
	return nil
}

func (p *PostgresTargetStore) Update(ctx context.Context, t Target) error {
	cfg := []byte(t.Config)
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	eventTypes := eventTypeArray(t.EventTypes)
	res, err := p.db.ExecContext(ctx, `
		UPDATE notification_targets
		   SET name=$2, kind=$3, config=$4, event_types=$5, enabled=$6, updated_at=NOW()
		 WHERE id=$1`,
		t.ID, t.Name, t.Kind, cfg, pq.Array(eventTypes), t.Enabled)
	if err != nil {
		return fmt.Errorf("notifier: update target: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTargetNotFound
	}
	return nil
}

func (p *PostgresTargetStore) Delete(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM notification_targets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("notifier: delete target: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTargetNotFound
	}
	return nil
}

// scannable is the subset of *sql.Rows / *sql.Row we need.
type scannable interface {
	Scan(dest ...any) error
}

func scanTarget(row scannable) (Target, error) {
	var (
		t          Target
		config     []byte
		eventTypes pq.StringArray
	)
	if err := row.Scan(&t.ID, &t.Name, &t.Kind, &config, &eventTypes, &t.Enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return Target{}, err
	}
	t.Config = json.RawMessage(config)
	if len(eventTypes) > 0 {
		t.EventTypes = make([]EventType, 0, len(eventTypes))
		for _, s := range eventTypes {
			t.EventTypes = append(t.EventTypes, EventType(s))
		}
	}
	return t, nil
}

func eventTypeArray(es []EventType) []string {
	if len(es) == 0 {
		return []string{}
	}
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e)
	}
	return out
}
