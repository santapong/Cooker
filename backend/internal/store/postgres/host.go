package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/store"
)

// HostStore implements store.HostStore using PostgreSQL.
type HostStore struct {
	db *sql.DB
}

func NewHostStore(db *sql.DB) *HostStore { return &HostStore{db: db} }

func (s *HostStore) List(ctx context.Context) ([]*model.Host, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, kind, reachability, docker_endpoint, kubeconfig_ref,
		       tailnet_ip, created_at, updated_at, version
		FROM hosts ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing hosts: %w", err)
	}
	defer rows.Close()

	var out []*model.Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *HostStore) Get(ctx context.Context, id string) (*model.Host, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, kind, reachability, docker_endpoint, kubeconfig_ref,
		       tailnet_ip, created_at, updated_at, version
		FROM hosts WHERE id=$1`, id)
	h, err := scanHost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("host %s: %w", id, store.ErrNotFound)
	}
	return h, err
}

func (s *HostStore) Create(ctx context.Context, h *model.Host) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO hosts (id, name, kind, reachability, docker_endpoint, kubeconfig_ref,
		                  tailnet_ip, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		h.ID, h.Name, h.Kind, h.Reachability, h.DockerEndpoint, h.KubeconfigRef,
		h.TailnetIP, h.CreatedAt, h.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating host: %w", err)
	}
	return nil
}

func (s *HostStore) Update(ctx context.Context, h *model.Host) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE hosts SET name=$2, kind=$3, reachability=$4, docker_endpoint=$5,
		                 kubeconfig_ref=$6, tailnet_ip=$7, updated_at=$8, version=version+1
		WHERE id=$1 AND version=$9`,
		h.ID, h.Name, h.Kind, h.Reachability, h.DockerEndpoint, h.KubeconfigRef,
		h.TailnetIP, h.UpdatedAt, h.Version)
	if err != nil {
		return fmt.Errorf("updating host: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM hosts WHERE id=$1)`, h.ID).Scan(&exists); err == nil && exists {
			return fmt.Errorf("host %s: %w", h.ID, store.ErrConflict)
		}
		return fmt.Errorf("host %s: %w", h.ID, store.ErrNotFound)
	}
	h.Version++
	return nil
}

func (s *HostStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM hosts WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("deleting host: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("host %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanHost(row scannable) (*model.Host, error) {
	h := &model.Host{}
	if err := row.Scan(
		&h.ID, &h.Name, &h.Kind, &h.Reachability, &h.DockerEndpoint,
		&h.KubeconfigRef, &h.TailnetIP, &h.CreatedAt, &h.UpdatedAt, &h.Version,
	); err != nil {
		return nil, err
	}
	return h, nil
}
