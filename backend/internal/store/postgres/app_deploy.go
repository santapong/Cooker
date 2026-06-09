package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// appDeployDefaultLimit caps ListByApp when the caller passes <= 0.
const appDeployDefaultLimit = 20

// AppDeployStore implements store.AppDeployStore using PostgreSQL.
type AppDeployStore struct {
	db *sql.DB
}

// NewAppDeployStore creates a PostgreSQL-backed deploy-history store.
func NewAppDeployStore(db *sql.DB) *AppDeployStore {
	return &AppDeployStore{db: db}
}

func (s *AppDeployStore) Create(ctx context.Context, d *model.AppDeploy) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO app_deploys
		   (id, app_id, run_id, pipeline_id, image_ref, digest, status, kind)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING created_at`,
		d.ID, d.AppID, d.RunID, d.PipelineID, d.ImageRef, d.Digest,
		string(d.Status), string(d.Kind)).Scan(&d.CreatedAt)
	if err != nil {
		return fmt.Errorf("creating app deploy: %w", err)
	}
	return nil
}

func (s *AppDeployStore) ListByApp(ctx context.Context, appID string, limit int) ([]*model.AppDeploy, error) {
	if limit <= 0 {
		limit = appDeployDefaultLimit
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, run_id, pipeline_id, image_ref, digest, status, kind, created_at
		   FROM app_deploys WHERE app_id = $1
		  ORDER BY created_at DESC
		  LIMIT $2`,
		appID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing app deploys: %w", err)
	}
	defer rows.Close()

	var out []*model.AppDeploy
	for rows.Next() {
		d, err := scanAppDeploy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *AppDeployStore) Get(ctx context.Context, id string) (*model.AppDeploy, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, run_id, pipeline_id, image_ref, digest, status, kind, created_at
		   FROM app_deploys WHERE id = $1`, id)
	d, err := scanAppDeploy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("app deploy %s: %w", id, store.ErrNotFound)
	}
	return d, err
}

func scanAppDeploy(row scannable) (*model.AppDeploy, error) {
	d := &model.AppDeploy{}
	var status, kind string
	if err := row.Scan(&d.ID, &d.AppID, &d.RunID, &d.PipelineID, &d.ImageRef,
		&d.Digest, &status, &kind, &d.CreatedAt); err != nil {
		return nil, err
	}
	d.Status = model.RunStatus(status)
	d.Kind = model.AppDeployKind(kind)
	return d, nil
}
