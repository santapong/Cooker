package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/store"
)

// EnvironmentStore implements store.EnvironmentStore using PostgreSQL.
type EnvironmentStore struct {
	db *sql.DB
}

// NewEnvironmentStore creates a PostgreSQL-backed environment store.
func NewEnvironmentStore(db *sql.DB) *EnvironmentStore {
	return &EnvironmentStore{db: db}
}

func (s *EnvironmentStore) List(ctx context.Context) ([]*model.Environment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, sort_order, target, promotion, variables, created_at FROM environments ORDER BY sort_order ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing environments: %w", err)
	}
	defer rows.Close()

	var out []*model.Environment
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *EnvironmentStore) Get(ctx context.Context, id string) (*model.Environment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, sort_order, target, promotion, variables, created_at FROM environments WHERE id = $1`, id)
	e, err := scanEnvironment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("environment %s: %w", id, store.ErrNotFound)
	}
	return e, err
}

func (s *EnvironmentStore) Create(ctx context.Context, e *model.Environment) error {
	targetJSON, err := json.Marshal(e.Target)
	if err != nil {
		return fmt.Errorf("marshal target: %w", err)
	}
	promoJSON, err := json.Marshal(e.Promotion)
	if err != nil {
		return fmt.Errorf("marshal promotion: %w", err)
	}
	varsJSON, err := json.Marshal(e.Variables)
	if err != nil {
		return fmt.Errorf("marshal variables: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO environments (id, name, sort_order, target, promotion, variables, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.ID, e.Name, e.Order, targetJSON, promoJSON, varsJSON, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("creating environment: %w", err)
	}
	return nil
}

func (s *EnvironmentStore) Update(ctx context.Context, e *model.Environment) error {
	targetJSON, err := json.Marshal(e.Target)
	if err != nil {
		return fmt.Errorf("marshal target: %w", err)
	}
	promoJSON, err := json.Marshal(e.Promotion)
	if err != nil {
		return fmt.Errorf("marshal promotion: %w", err)
	}
	varsJSON, err := json.Marshal(e.Variables)
	if err != nil {
		return fmt.Errorf("marshal variables: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE environments SET name=$2, sort_order=$3, target=$4, promotion=$5, variables=$6 WHERE id=$1`,
		e.ID, e.Name, e.Order, targetJSON, promoJSON, varsJSON)
	if err != nil {
		return fmt.Errorf("updating environment: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("environment %s: %w", e.ID, store.ErrNotFound)
	}
	return nil
}

func (s *EnvironmentStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM environments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting environment: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("environment %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanEnvironment(row scannable) (*model.Environment, error) {
	e := &model.Environment{}
	var targetJSON, promoJSON, varsJSON []byte
	if err := row.Scan(&e.ID, &e.Name, &e.Order, &targetJSON, &promoJSON, &varsJSON, &e.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(targetJSON, &e.Target); err != nil {
		return nil, fmt.Errorf("unmarshal target: %w", err)
	}
	if err := json.Unmarshal(promoJSON, &e.Promotion); err != nil {
		return nil, fmt.Errorf("unmarshal promotion: %w", err)
	}
	if err := json.Unmarshal(varsJSON, &e.Variables); err != nil {
		return nil, fmt.Errorf("unmarshal variables: %w", err)
	}
	return e, nil
}
