package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// appCanaryColumns is the canonical SELECT-list for an AppCanary. Kept
// in one place so Get / GetActive / ListProgressing / scan stay in
// lock-step when a column is added.
const appCanaryColumns = `id, app_id, run_id, stable_image, canary_image, weight, status,
	auto_promote, health_window_seconds, healthy, message,
	promote_after, started_at, updated_at, resolved_at`

// AppCanaryStore implements store.AppCanaryStore using PostgreSQL.
type AppCanaryStore struct {
	db *sql.DB
}

// NewAppCanaryStore creates a PostgreSQL-backed canary-state store.
func NewAppCanaryStore(db *sql.DB) *AppCanaryStore {
	return &AppCanaryStore{db: db}
}

func (s *AppCanaryStore) Create(ctx context.Context, c *model.AppCanary) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO app_canaries
		   (id, app_id, run_id, stable_image, canary_image, weight, status,
		    auto_promote, health_window_seconds, healthy, message, promote_after)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 RETURNING started_at, updated_at`,
		c.ID, c.AppID, c.RunID, c.StableImage, c.CanaryImage, c.Weight, string(c.Status),
		c.AutoPromote, c.HealthWindowSeconds, c.Healthy, c.Message, c.PromoteAfter).
		Scan(&c.StartedAt, &c.UpdatedAt)
	if err != nil {
		var pqErr *pq.Error
		// 23505 = unique_violation: the partial unique index
		// (uq_app_canaries_active) rejects a second progressing canary for
		// the same app. Map to ErrConflict → HTTP 409.
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return fmt.Errorf("app %s: canary in flight: %w", c.AppID, store.ErrConflict)
		}
		return fmt.Errorf("creating app canary: %w", err)
	}
	return nil
}

func (s *AppCanaryStore) Get(ctx context.Context, id string) (*model.AppCanary, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+appCanaryColumns+` FROM app_canaries WHERE id = $1`, id)
	c, err := scanAppCanary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("app canary %s: %w", id, store.ErrNotFound)
	}
	return c, err
}

func (s *AppCanaryStore) GetActive(ctx context.Context, appID string) (*model.AppCanary, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+appCanaryColumns+`
		   FROM app_canaries WHERE app_id = $1 AND status = 'progressing'`, appID)
	c, err := scanAppCanary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("app %s: no active canary: %w", appID, store.ErrNotFound)
	}
	return c, err
}

func (s *AppCanaryStore) LatestPromoted(ctx context.Context, appID string) (*model.AppCanary, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+appCanaryColumns+`
		   FROM app_canaries WHERE app_id = $1 AND status = 'promoted'
		  ORDER BY resolved_at DESC NULLS LAST LIMIT 1`, appID)
	c, err := scanAppCanary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("app %s: no promoted canary: %w", appID, store.ErrNotFound)
	}
	return c, err
}

func (s *AppCanaryStore) Update(ctx context.Context, c *model.AppCanary) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE app_canaries
		    SET weight=$2, status=$3, healthy=$4, message=$5,
		        promote_after=$6, resolved_at=$7, updated_at=NOW()
		  WHERE id=$1`,
		c.ID, c.Weight, string(c.Status), c.Healthy, c.Message,
		c.PromoteAfter, c.ResolvedAt)
	if err != nil {
		return fmt.Errorf("updating app canary: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app canary %s: %w", c.ID, store.ErrNotFound)
	}
	return nil
}

func (s *AppCanaryStore) ListProgressing(ctx context.Context) ([]*model.AppCanary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+appCanaryColumns+`
		   FROM app_canaries WHERE status = 'progressing'
		  ORDER BY started_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing progressing canaries: %w", err)
	}
	defer rows.Close()

	out := make([]*model.AppCanary, 0)
	for rows.Next() {
		c, err := scanAppCanary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanAppCanary(row scannable) (*model.AppCanary, error) {
	c := &model.AppCanary{}
	var status string
	var promoteAfter, resolvedAt sql.NullTime
	if err := row.Scan(
		&c.ID, &c.AppID, &c.RunID, &c.StableImage, &c.CanaryImage, &c.Weight, &status,
		&c.AutoPromote, &c.HealthWindowSeconds, &c.Healthy, &c.Message,
		&promoteAfter, &c.StartedAt, &c.UpdatedAt, &resolvedAt,
	); err != nil {
		return nil, err
	}
	c.Status = model.CanaryStatus(status)
	if promoteAfter.Valid {
		t := promoteAfter.Time
		c.PromoteAfter = &t
	}
	if resolvedAt.Valid {
		t := resolvedAt.Time
		c.ResolvedAt = &t
	}
	return c, nil
}
