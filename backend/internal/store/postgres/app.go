package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/store"
)

// appColumns is the canonical SELECT-list for an App. Centralised so
// List / Get / GetByRepo / scanApp stay in lock-step when a column
// is added (e.g., the health_* trio added in Week 1).
const appColumns = `id, name, description, github_repo, branch, build_plan, deploy_target,
	registry_ref, environment_id, webhook_secret, auto_deploy,
	created_at, updated_at, version,
	COALESCE(health_status, 'unknown') AS health_status, health_checked_at, COALESCE(health_message, '') AS health_message`

// AppStore implements store.AppStore using PostgreSQL. Webhook
// secrets are stored as base64 in a separate column — encryption
// already happened at the handler layer via crypto.Codec.
type AppStore struct {
	db *sql.DB
}

func NewAppStore(db *sql.DB) *AppStore { return &AppStore{db: db} }

func (s *AppStore) List(ctx context.Context) ([]*model.App, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+appColumns+`
		FROM apps ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing apps: %w", err)
	}
	defer rows.Close()

	var out []*model.App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *AppStore) Get(ctx context.Context, id string) (*model.App, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+appColumns+`
		FROM apps WHERE id=$1`, id)
	a, err := scanApp(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	return a, err
}

func (s *AppStore) GetByRepo(ctx context.Context, repo, branch string) (*model.App, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+appColumns+`
		FROM apps WHERE github_repo=$1 AND branch=$2`, repo, branch)
	a, err := scanApp(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("app %s@%s: %w", repo, branch, store.ErrNotFound)
	}
	return a, err
}

func (s *AppStore) Create(ctx context.Context, a *model.App) error {
	bp, dt, err := marshalAppJSON(a)
	if err != nil {
		return err
	}
	secret := base64StdEncode(a.WebhookSecret)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO apps (id, name, description, github_repo, branch, build_plan, deploy_target,
		                  registry_ref, environment_id, webhook_secret, auto_deploy,
		                  created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		a.ID, a.Name, a.Description, a.GitHubRepo, a.Branch, bp, dt,
		a.RegistryRef, a.EnvironmentID, secret, a.AutoDeploy,
		a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating app: %w", err)
	}
	return nil
}

func (s *AppStore) Update(ctx context.Context, a *model.App) error {
	bp, dt, err := marshalAppJSON(a)
	if err != nil {
		return err
	}
	secret := base64StdEncode(a.WebhookSecret)
	res, err := s.db.ExecContext(ctx, `
		UPDATE apps SET name=$2, description=$3, github_repo=$4, branch=$5,
		                build_plan=$6, deploy_target=$7, registry_ref=$8,
		                environment_id=$9, webhook_secret=$10, auto_deploy=$11,
		                updated_at=$12, version=version+1
		WHERE id=$1 AND version=$13`,
		a.ID, a.Name, a.Description, a.GitHubRepo, a.Branch, bp, dt,
		a.RegistryRef, a.EnvironmentID, secret, a.AutoDeploy, a.UpdatedAt, a.Version)
	if err != nil {
		return fmt.Errorf("updating app: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM apps WHERE id=$1)`, a.ID).Scan(&exists); err == nil && exists {
			return fmt.Errorf("app %s: %w", a.ID, store.ErrConflict)
		}
		return fmt.Errorf("app %s: %w", a.ID, store.ErrNotFound)
	}
	a.Version++
	return nil
}

func (s *AppStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM apps WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("deleting app: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// UpdateHealth writes the latest probe verdict without touching
// version. Health is observational, not user-visible config — a
// concurrent Update from the user must not lose its race with a
// background probe write.
func (s *AppStore) UpdateHealth(ctx context.Context, id string, status model.AppHealth, msg string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE apps SET health_status=$2, health_checked_at=$3, health_message=$4
		WHERE id=$1`, id, string(status), at, msg)
	if err != nil {
		return fmt.Errorf("updating app health: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func marshalAppJSON(a *model.App) (bp, dt []byte, err error) {
	bp, err = json.Marshal(a.BuildPlan)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal build_plan: %w", err)
	}
	dt, err = json.Marshal(a.DeployTarget)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal deploy_target: %w", err)
	}
	return bp, dt, nil
}

func scanApp(row scannable) (*model.App, error) {
	a := &model.App{}
	var bp, dt []byte
	var secretB64 string
	var healthStatus string
	var healthCheckedAt sql.NullTime
	var healthMessage string
	if err := row.Scan(
		&a.ID, &a.Name, &a.Description, &a.GitHubRepo, &a.Branch,
		&bp, &dt, &a.RegistryRef, &a.EnvironmentID, &secretB64, &a.AutoDeploy,
		&a.CreatedAt, &a.UpdatedAt, &a.Version,
		&healthStatus, &healthCheckedAt, &healthMessage,
	); err != nil {
		return nil, err
	}
	a.HealthStatus = model.AppHealth(healthStatus)
	if healthCheckedAt.Valid {
		t := healthCheckedAt.Time
		a.HealthCheckedAt = &t
	}
	a.HealthMessage = healthMessage
	if len(bp) > 0 && string(bp) != "null" {
		if err := json.Unmarshal(bp, &a.BuildPlan); err != nil {
			return nil, fmt.Errorf("unmarshal build_plan: %w", err)
		}
	}
	if err := json.Unmarshal(dt, &a.DeployTarget); err != nil {
		return nil, fmt.Errorf("unmarshal deploy_target: %w", err)
	}
	if secretB64 != "" {
		sec, err := base64StdDecode(secretB64)
		if err != nil {
			return nil, fmt.Errorf("decode webhook secret: %w", err)
		}
		a.WebhookSecret = sec
	}
	return a, nil
}
