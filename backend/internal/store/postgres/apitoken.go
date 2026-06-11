package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// APITokenStore implements store.APITokenStore using PostgreSQL. Tokens
// live in the api_tokens table (migration 023). Only the SHA-256 hash is
// stored; GetByHash rides the unique hash index on the auth hot path.
type APITokenStore struct {
	db *sql.DB
}

// NewAPITokenStore creates a PostgreSQL-backed API-token store.
func NewAPITokenStore(db *sql.DB) *APITokenStore {
	return &APITokenStore{db: db}
}

func (s *APITokenStore) Create(ctx context.Context, t *model.APIToken) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO api_tokens
		   (id, name, display_prefix, hash, role, created_by_sub, created_by_email, expires_at, last_used_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 RETURNING created_at`,
		t.ID, t.Name, t.DisplayPrefix, t.Hash, t.Role,
		t.CreatedBySub, t.CreatedByEmail, nullTime(t.ExpiresAt), nullTime(t.LastUsedAt)).
		Scan(&t.CreatedAt)
	if err != nil {
		var pqErr *pq.Error
		// 23505 = unique_violation on the hash index: two tokens cannot
		// share a hash. Vanishingly unlikely with 32 bytes of entropy, but
		// map it to ErrConflict so a collision is reported, not swallowed.
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return fmt.Errorf("api token hash: %w", store.ErrConflict)
		}
		return fmt.Errorf("creating api token: %w", err)
	}
	return nil
}

func (s *APITokenStore) ListByCreator(ctx context.Context, createdBySub string) ([]*model.APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, display_prefix, hash, role, created_by_sub, created_by_email,
		        created_at, expires_at, last_used_at
		   FROM api_tokens WHERE created_by_sub = $1
		  ORDER BY created_at DESC, id ASC`, createdBySub)
	if err != nil {
		return nil, fmt.Errorf("listing api tokens by creator: %w", err)
	}
	return scanAPITokens(rows)
}

func (s *APITokenStore) ListAll(ctx context.Context) ([]*model.APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, display_prefix, hash, role, created_by_sub, created_by_email,
		        created_at, expires_at, last_used_at
		   FROM api_tokens
		  ORDER BY created_at DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing all api tokens: %w", err)
	}
	return scanAPITokens(rows)
}

func (s *APITokenStore) GetByHash(ctx context.Context, hash string) (*model.APIToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_prefix, hash, role, created_by_sub, created_by_email,
		        created_at, expires_at, last_used_at
		   FROM api_tokens WHERE hash = $1`, hash)
	t, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("api token by hash: %w", store.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *APITokenStore) Get(ctx context.Context, id string) (*model.APIToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_prefix, hash, role, created_by_sub, created_by_email,
		        created_at, expires_at, last_used_at
		   FROM api_tokens WHERE id = $1`, id)
	t, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("api token %s: %w", id, store.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *APITokenStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting api token %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api token %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *APITokenStore) TouchLastUsed(ctx context.Context, id string, ts time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = $2 WHERE id = $1`, id, ts)
	if err != nil {
		return fmt.Errorf("touching api token %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api token %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanAPITokens(rows *sql.Rows) ([]*model.APIToken, error) {
	defer rows.Close()
	out := make([]*model.APIToken, 0)
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanAPIToken(row scannable) (*model.APIToken, error) {
	t := &model.APIToken{}
	var expiresAt, lastUsedAt sql.NullTime
	if err := row.Scan(&t.ID, &t.Name, &t.DisplayPrefix, &t.Hash, &t.Role,
		&t.CreatedBySub, &t.CreatedByEmail, &t.CreatedAt, &expiresAt, &lastUsedAt); err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		e := expiresAt.Time
		t.ExpiresAt = &e
	}
	if lastUsedAt.Valid {
		l := lastUsedAt.Time
		t.LastUsedAt = &l
	}
	return t, nil
}
