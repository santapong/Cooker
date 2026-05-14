package templates

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNotFound is returned by Get when the ID is unknown.
var ErrNotFound = errors.New("templates: template not found")

// Store persists templates. Two implementations: Postgres (production)
// and Memory (tests / dev-mode catalog seeding).
type Store interface {
	// List returns enabled templates only (the gallery's default
	// view). Operators who want to manage disabled rows go through
	// SQL directly until the admin UI exists.
	List(ctx context.Context) ([]Template, error)
	Get(ctx context.Context, id string) (Template, error)
	Create(ctx context.Context, t Template) error
	Update(ctx context.Context, t Template) error
	Delete(ctx context.Context, id string) error
}

// MemoryStore is an in-process Store for tests.
type MemoryStore struct {
	mu        sync.RWMutex
	templates map[string]Template
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{templates: make(map[string]Template)}
}

func (m *MemoryStore) List(_ context.Context) ([]Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Template, 0, len(m.templates))
	for _, t := range m.templates {
		if t.Enabled {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.templates[id]
	if !ok {
		return Template{}, ErrNotFound
	}
	return t, nil
}

func (m *MemoryStore) Create(_ context.Context, t Template) error {
	if t.ID == "" {
		return fmt.Errorf("templates: Create with empty ID")
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.templates[t.ID]; exists {
		return fmt.Errorf("templates: duplicate id %s", t.ID)
	}
	m.templates[t.ID] = t
	return nil
}

func (m *MemoryStore) Update(_ context.Context, t Template) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.templates[t.ID]; !ok {
		return ErrNotFound
	}
	t.UpdatedAt = time.Now().UTC()
	m.templates[t.ID] = t
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.templates[id]; !ok {
		return ErrNotFound
	}
	delete(m.templates, id)
	return nil
}

// PostgresStore is the production Store backed by the
// pipeline_templates table (migration 013).
type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore { return &PostgresStore{db: db} }

func (p *PostgresStore) List(ctx context.Context) ([]Template, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, name, description, category, schema, icon_url,
		       enabled, created_at, updated_at
		  FROM pipeline_templates
		 WHERE enabled = TRUE
		 ORDER BY category, name`)
	if err != nil {
		return nil, fmt.Errorf("templates: list: %w", err)
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *PostgresStore) Get(ctx context.Context, id string) (Template, error) {
	row := p.db.QueryRowContext(ctx, `
		SELECT id, name, description, category, schema, icon_url,
		       enabled, created_at, updated_at
		  FROM pipeline_templates WHERE id = $1`, id)
	t, err := scanTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, ErrNotFound
	}
	return t, err
}

func (p *PostgresStore) Create(ctx context.Context, t Template) error {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	schema := []byte(t.Schema)
	if len(schema) == 0 {
		schema = []byte("{}")
	}
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO pipeline_templates
		  (id, name, description, category, schema, icon_url,
		   enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		t.ID, t.Name, t.Description, t.Category, schema, t.IconURL,
		t.Enabled, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("templates: create: %w", err)
	}
	return nil
}

func (p *PostgresStore) Update(ctx context.Context, t Template) error {
	schema := []byte(t.Schema)
	if len(schema) == 0 {
		schema = []byte("{}")
	}
	res, err := p.db.ExecContext(ctx, `
		UPDATE pipeline_templates
		   SET name=$2, description=$3, category=$4, schema=$5,
		       icon_url=$6, enabled=$7, updated_at=NOW()
		 WHERE id=$1`,
		t.ID, t.Name, t.Description, t.Category, schema, t.IconURL, t.Enabled)
	if err != nil {
		return fmt.Errorf("templates: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresStore) Delete(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM pipeline_templates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("templates: delete: %w", err)
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

func scanTemplate(row scannable) (Template, error) {
	var (
		t      Template
		schema []byte
	)
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Category, &schema,
		&t.IconURL, &t.Enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return Template{}, err
	}
	t.Schema = json.RawMessage(schema)
	return t, nil
}
