package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// RegistryConfigStore implements store.RegistryConfigStore using
// PostgreSQL. The auth password lives in secrets.Manager; only
// password_ref is persisted on the row (see service.RegistryConfigService).
type RegistryConfigStore struct {
	db *sql.DB
}

func NewRegistryConfigStore(db *sql.DB) *RegistryConfigStore { return &RegistryConfigStore{db: db} }

// registryConfigColumns is the SELECT projection shared by List and
// Get. Keep in sync with scanRegistryConfig.
const registryConfigColumns = `id, name, url, username, password_ref, created_at, updated_at`

func (s *RegistryConfigStore) List(ctx context.Context) ([]*model.RegistryConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+registryConfigColumns+`
		FROM registry_configs ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing registry configs: %w", err)
	}
	defer rows.Close()

	var out []*model.RegistryConfig
	for rows.Next() {
		r, err := scanRegistryConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *RegistryConfigStore) Get(ctx context.Context, id string) (*model.RegistryConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+registryConfigColumns+`
		FROM registry_configs WHERE id=$1`, id)
	r, err := scanRegistryConfig(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("registry config %s: %w", id, store.ErrNotFound)
	}
	return r, err
}

func (s *RegistryConfigStore) Create(ctx context.Context, r *model.RegistryConfig) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO registry_configs (id, name, url, username, password_ref, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		r.ID, r.Name, r.URL, r.Username, r.PasswordRef, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating registry config: %w", err)
	}
	return nil
}

func (s *RegistryConfigStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM registry_configs WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("deleting registry config: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("registry config %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanRegistryConfig(row scannable) (*model.RegistryConfig, error) {
	r := &model.RegistryConfig{}
	if err := row.Scan(
		&r.ID, &r.Name, &r.URL, &r.Username, &r.PasswordRef,
		&r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return r, nil
}

// ClusterConfigStore implements store.ClusterConfigStore using
// PostgreSQL. The kubeconfig body lives in secrets.Manager; only
// kubeconfig_ref is persisted on the row.
type ClusterConfigStore struct {
	db *sql.DB
}

func NewClusterConfigStore(db *sql.DB) *ClusterConfigStore { return &ClusterConfigStore{db: db} }

// clusterConfigColumns is the SELECT projection shared by List and
// Get. Keep in sync with scanClusterConfig.
const clusterConfigColumns = `id, name, context, kubeconfig_ref, created_at, updated_at`

func (s *ClusterConfigStore) List(ctx context.Context) ([]*model.ClusterConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+clusterConfigColumns+`
		FROM cluster_configs ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing cluster configs: %w", err)
	}
	defer rows.Close()

	var out []*model.ClusterConfig
	for rows.Next() {
		c, err := scanClusterConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *ClusterConfigStore) Get(ctx context.Context, id string) (*model.ClusterConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+clusterConfigColumns+`
		FROM cluster_configs WHERE id=$1`, id)
	c, err := scanClusterConfig(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("cluster config %s: %w", id, store.ErrNotFound)
	}
	return c, err
}

func (s *ClusterConfigStore) Create(ctx context.Context, c *model.ClusterConfig) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cluster_configs (id, name, context, kubeconfig_ref, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		c.ID, c.Name, c.Context, c.KubeconfigRef, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating cluster config: %w", err)
	}
	return nil
}

func (s *ClusterConfigStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM cluster_configs WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("deleting cluster config: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("cluster config %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanClusterConfig(row scannable) (*model.ClusterConfig, error) {
	c := &model.ClusterConfig{}
	if err := row.Scan(
		&c.ID, &c.Name, &c.Context, &c.KubeconfigRef,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return c, nil
}
