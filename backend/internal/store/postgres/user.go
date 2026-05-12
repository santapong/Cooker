package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// UserStore implements store.UserStore using PostgreSQL.
type UserStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) *UserStore { return &UserStore{db: db} }

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, email, name, password_hash, role, created_at, updated_at
		FROM users WHERE LOWER(email)=LOWER($1)`, strings.TrimSpace(email))
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user %s: %w", email, store.ErrNotFound)
	}
	return u, err
}

func (s *UserStore) GetByID(ctx context.Context, id string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, email, name, password_hash, role, created_at, updated_at
		FROM users WHERE id=$1`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user %s: %w", id, store.ErrNotFound)
	}
	return u, err
}

func (s *UserStore) Create(ctx context.Context, u *model.User) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, name, password_hash, role, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		u.ID, strings.TrimSpace(u.Email), u.Name, u.PasswordHash, u.Role, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating user: %w", err)
	}
	return nil
}

func (s *UserStore) Update(ctx context.Context, u *model.User) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET email=$2, name=$3, password_hash=$4, role=$5, updated_at=$6
		WHERE id=$1`,
		u.ID, strings.TrimSpace(u.Email), u.Name, u.PasswordHash, u.Role, u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %s: %w", u.ID, store.ErrNotFound)
	}
	return nil
}

func (s *UserStore) Count(ctx context.Context) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("counting users: %w", err)
	}
	return n, nil
}

func scanUser(row scannable) (*model.User, error) {
	u := &model.User{}
	if err := row.Scan(
		&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return u, nil
}
