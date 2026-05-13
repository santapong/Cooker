package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
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
		`SELECT id, name, sort_order, target, promotion, variables, secrets, created_at, version FROM environments ORDER BY sort_order ASC`)
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
		`SELECT id, name, sort_order, target, promotion, variables, secrets, created_at, version FROM environments WHERE id = $1`, id)
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
	varsJSON, err := json.Marshal(e.PlainVars)
	if err != nil {
		return fmt.Errorf("marshal plain vars: %w", err)
	}
	secretsJSON, err := marshalSecrets(e.Secrets)
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO environments (id, name, sort_order, target, promotion, variables, secrets, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.Name, e.Order, targetJSON, promoJSON, varsJSON, secretsJSON, e.CreatedAt)
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
	varsJSON, err := json.Marshal(e.PlainVars)
	if err != nil {
		return fmt.Errorf("marshal plain vars: %w", err)
	}
	secretsJSON, err := marshalSecrets(e.Secrets)
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE environments SET name=$2, sort_order=$3, target=$4, promotion=$5, variables=$6, secrets=$7, version=version+1
		 WHERE id=$1 AND version=$8`,
		e.ID, e.Name, e.Order, targetJSON, promoJSON, varsJSON, secretsJSON, e.Version)
	if err != nil {
		return fmt.Errorf("updating environment: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM environments WHERE id=$1)`, e.ID).Scan(&exists); err == nil && exists {
			return fmt.Errorf("environment %s: %w", e.ID, store.ErrConflict)
		}
		return fmt.Errorf("environment %s: %w", e.ID, store.ErrNotFound)
	}
	e.Version++
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
	var targetJSON, promoJSON, varsJSON, secretsJSON []byte
	if err := row.Scan(&e.ID, &e.Name, &e.Order, &targetJSON, &promoJSON, &varsJSON, &secretsJSON, &e.CreatedAt, &e.Version); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(targetJSON, &e.Target); err != nil {
		return nil, fmt.Errorf("unmarshal target: %w", err)
	}
	if err := json.Unmarshal(promoJSON, &e.Promotion); err != nil {
		return nil, fmt.Errorf("unmarshal promotion: %w", err)
	}
	if err := json.Unmarshal(varsJSON, &e.PlainVars); err != nil {
		return nil, fmt.Errorf("unmarshal plain vars: %w", err)
	}
	secrets, err := unmarshalSecrets(secretsJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal secrets: %w", err)
	}
	e.Secrets = secrets
	return e, nil
}

// marshalSecrets encodes a map[string][]byte as JSONB where each
// value is base64'd. JSONB can't hold raw bytes, and using base64
// keeps the column human-inspectable.
func marshalSecrets(m map[string][]byte) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	enc := make(map[string]string, len(m))
	for k, v := range m {
		enc[k] = base64StdEncode(v)
	}
	return json.Marshal(enc)
}

func unmarshalSecrets(b []byte) (map[string][]byte, error) {
	if len(b) == 0 || string(b) == "{}" {
		return nil, nil
	}
	var enc map[string]string
	if err := json.Unmarshal(b, &enc); err != nil {
		return nil, err
	}
	if len(enc) == 0 {
		return nil, nil
	}
	out := make(map[string][]byte, len(enc))
	for k, v := range enc {
		dec, err := base64StdDecode(v)
		if err != nil {
			return nil, fmt.Errorf("decode secret %q: %w", k, err)
		}
		out[k] = dec
	}
	return out, nil
}
