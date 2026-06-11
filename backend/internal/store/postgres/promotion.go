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

// PromotionStore implements store.PromotionStore using PostgreSQL.
// Promotions and their approvals live in the run_promotions and
// promotion_approvals tables (migration 020). See
// docs/adr/0005-promotion-approval-persistence.md.
type PromotionStore struct {
	db *sql.DB
}

// NewPromotionStore creates a PostgreSQL-backed promotion store.
func NewPromotionStore(db *sql.DB) *PromotionStore {
	return &PromotionStore{db: db}
}

func (s *PromotionStore) CreatePromotion(ctx context.Context, p *model.RunPromotion) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO run_promotions
		   (id, run_id, pipeline_id, environment_id, status, strategy,
		    required_approvers, requested_by, promoted_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 RETURNING created_at, updated_at`,
		p.ID, p.RunID, p.PipelineID, p.EnvironmentID, string(p.Status), p.Strategy,
		p.RequiredApprovers, p.RequestedBy, nullTime(p.PromotedAt)).
		Scan(&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		var pqErr *pq.Error
		// 23505 = unique_violation: a promotion already exists for this
		// (run, environment). Map to ErrConflict so promote is idempotent.
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return fmt.Errorf("promotion %s/%s: %w", p.RunID, p.EnvironmentID, store.ErrConflict)
		}
		return fmt.Errorf("creating promotion: %w", err)
	}
	return nil
}

func (s *PromotionStore) GetPromotion(ctx context.Context, runID, environmentID string) (*model.RunPromotion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, run_id, pipeline_id, environment_id, status, strategy,
		        required_approvers, requested_by, promoted_at, created_at, updated_at
		   FROM run_promotions WHERE run_id = $1 AND environment_id = $2`,
		runID, environmentID)
	p, err := scanPromotion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("promotion %s/%s: %w", runID, environmentID, store.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	approvals, err := s.listApprovals(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	p.Approvals = approvals
	return p, nil
}

func (s *PromotionStore) ListPromotions(ctx context.Context, runID string) ([]*model.RunPromotion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, pipeline_id, environment_id, status, strategy,
		        required_approvers, requested_by, promoted_at, created_at, updated_at
		   FROM run_promotions WHERE run_id = $1
		  ORDER BY created_at ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("listing promotions: %w", err)
	}
	defer rows.Close()

	var out []*model.RunPromotion
	for rows.Next() {
		p, err := scanPromotion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Hydrate approvals per promotion. The promotion count per run is
	// small (one per environment), so a query each is fine.
	for _, p := range out {
		approvals, err := s.listApprovals(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		p.Approvals = approvals
	}
	return out, nil
}

func (s *PromotionStore) UpdatePromotionStatus(ctx context.Context, id string, status model.PromotionStatus, promotedAt *time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE run_promotions
		    SET status = $2,
		        promoted_at = COALESCE($3, promoted_at),
		        updated_at = NOW()
		  WHERE id = $1`,
		id, string(status), nullTime(promotedAt))
	if err != nil {
		return fmt.Errorf("updating promotion %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("promotion %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *PromotionStore) AddApproval(ctx context.Context, a *model.PromotionApproval) (bool, int, error) {
	// ON CONFLICT DO NOTHING makes a repeat approval by the same identity
	// a no-op rather than a unique-violation error. RETURNING id fires
	// only when a row was actually inserted, so a missing scan row means
	// "already approved".
	var insertedID string
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO promotion_approvals
		   (id, promotion_id, approver_sub, approver_email, note)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (promotion_id, approver_sub) DO NOTHING
		 RETURNING id`,
		a.ID, a.PromotionID, a.ApproverSub, a.ApproverEmail, a.Note).Scan(&insertedID)
	added := true
	if errors.Is(err, sql.ErrNoRows) {
		added = false
	} else if err != nil {
		var pqErr *pq.Error
		// 23503 = foreign_key_violation: the promotion is gone.
		if errors.As(err, &pqErr) && pqErr.Code == "23503" {
			return false, 0, fmt.Errorf("promotion %s: %w", a.PromotionID, store.ErrNotFound)
		}
		return false, 0, fmt.Errorf("adding approval: %w", err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM promotion_approvals WHERE promotion_id = $1`,
		a.PromotionID).Scan(&count); err != nil {
		return added, 0, fmt.Errorf("counting approvals: %w", err)
	}
	return added, count, nil
}

func (s *PromotionStore) listApprovals(ctx context.Context, promotionID string) ([]model.PromotionApproval, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, promotion_id, approver_sub, approver_email, note, created_at
		   FROM promotion_approvals WHERE promotion_id = $1
		  ORDER BY created_at ASC`, promotionID)
	if err != nil {
		return nil, fmt.Errorf("listing approvals: %w", err)
	}
	defer rows.Close()

	var out []model.PromotionApproval
	for rows.Next() {
		var a model.PromotionApproval
		if err := rows.Scan(&a.ID, &a.PromotionID, &a.ApproverSub, &a.ApproverEmail, &a.Note, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanPromotion(row scannable) (*model.RunPromotion, error) {
	p := &model.RunPromotion{}
	var status string
	var promotedAt sql.NullTime
	if err := row.Scan(&p.ID, &p.RunID, &p.PipelineID, &p.EnvironmentID, &status,
		&p.Strategy, &p.RequiredApprovers, &p.RequestedBy, &promotedAt,
		&p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.Status = model.PromotionStatus(status)
	if promotedAt.Valid {
		t := promotedAt.Time
		p.PromotedAt = &t
	}
	return p, nil
}
