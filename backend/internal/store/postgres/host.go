package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// HostStore implements store.HostStore using PostgreSQL.
type HostStore struct {
	db *sql.DB
}

func NewHostStore(db *sql.DB) *HostStore { return &HostStore{db: db} }

// hostColumns lists the SELECT projection shared by List and Get. Keep
// in sync with scanHost — both must order the same way.
const hostColumns = `id, name, kind, reachability, docker_endpoint, kubeconfig_ref,
		       tailnet_ip, ssh_endpoint, ssh_user, ssh_port, ssh_private_key_ref,
		       ssh_known_host_key, ssh_strict_host_key,
		       created_at, updated_at, version`

func (s *HostStore) List(ctx context.Context, limit, offset int) ([]*model.Host, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+hostColumns+`
		FROM hosts ORDER BY name ASC LIMIT $1 OFFSET $2`,
		limitArg(limit), clampOffset(offset))
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
		SELECT `+hostColumns+`
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
		                  tailnet_ip, ssh_endpoint, ssh_user, ssh_port, ssh_private_key_ref,
		                  ssh_known_host_key, ssh_strict_host_key,
		                  created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		h.ID, h.Name, h.Kind, h.Reachability, h.DockerEndpoint, h.KubeconfigRef,
		h.TailnetIP, h.SSHEndpoint, h.SSHUser, h.SSHPort, h.SSHPrivateKeyRef,
		h.SSHKnownHostKey, h.SSHStrictHostKey,
		h.CreatedAt, h.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating host: %w", err)
	}
	return nil
}

func (s *HostStore) Update(ctx context.Context, h *model.Host) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE hosts SET name=$2, kind=$3, reachability=$4, docker_endpoint=$5,
		                 kubeconfig_ref=$6, tailnet_ip=$7,
		                 ssh_endpoint=$8, ssh_user=$9, ssh_port=$10,
		                 ssh_private_key_ref=$11, ssh_known_host_key=$12,
		                 ssh_strict_host_key=$13,
		                 updated_at=$14, version=version+1
		WHERE id=$1 AND version=$15`,
		h.ID, h.Name, h.Kind, h.Reachability, h.DockerEndpoint, h.KubeconfigRef,
		h.TailnetIP, h.SSHEndpoint, h.SSHUser, h.SSHPort, h.SSHPrivateKeyRef,
		h.SSHKnownHostKey, h.SSHStrictHostKey,
		h.UpdatedAt, h.Version)
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
		&h.KubeconfigRef, &h.TailnetIP,
		&h.SSHEndpoint, &h.SSHUser, &h.SSHPort, &h.SSHPrivateKeyRef,
		&h.SSHKnownHostKey, &h.SSHStrictHostKey,
		&h.CreatedAt, &h.UpdatedAt, &h.Version,
	); err != nil {
		return nil, err
	}
	return h, nil
}
