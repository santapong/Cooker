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

// GetPromotion fetches a promotion and its approvals inside a single
// read transaction (TOCTOU fix, DA-M): the promotion row and the
// approval rows are read from the same consistent snapshot, so a
// concurrent AddApproval cannot produce a count that disagrees with
// the approval list.
func (s *PromotionStore) GetPromotion(ctx context.Context, runID, environmentID string) (*model.RunPromotion, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("getting promotion: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // read-only rollback is always safe

	row := tx.QueryRowContext(ctx,
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
	approvals, err := listApprovalsTx(ctx, tx, p.ID)
	if err != nil {
		return nil, err
	}
	p.Approvals = approvals
	return p, nil
}

// ListPromotions returns all promotions for a run with their approvals.
// Approvals are fetched in a single batched query (DA-H3 fix) rather
// than one query per promotion, to avoid the N+1 pattern.
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
	var ids []string
	for rows.Next() {
		p, err := scanPromotion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
		ids = append(ids, p.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}

	// Fetch all approvals for this run's promotions in one query (DA-H3).
	approvalMap, err := s.listApprovalsBatch(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, p := range out {
		p.Approvals = approvalMap[p.ID]
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

// AddApproval inserts an approval row and returns the post-insert count.
// Both statements execute inside a single transaction (DA-H2 fix) so
// concurrent approvals cannot observe a count that omits a just-inserted
// peer row, which would cause a gate to stall permanently.
func (s *PromotionStore) AddApproval(ctx context.Context, a *model.PromotionApproval) (bool, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, fmt.Errorf("adding approval: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// ON CONFLICT DO NOTHING makes a repeat approval by the same identity
	// a no-op rather than a unique-violation error. RETURNING id fires
	// only when a row was actually inserted, so a missing scan row means
	// "already approved".
	var insertedID string
	err = tx.QueryRowContext(ctx,
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
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM promotion_approvals WHERE promotion_id = $1`,
		a.PromotionID).Scan(&count); err != nil {
		return false, 0, fmt.Errorf("counting approvals: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, 0, fmt.Errorf("adding approval: commit: %w", err)
	}
	return added, count, nil
}

// listApprovalsTx fetches approvals for a single promotion within the
// supplied transaction (used by GetPromotion's TOCTOU-safe read tx).
func listApprovalsTx(ctx context.Context, tx *sql.Tx, promotionID string) ([]model.PromotionApproval, error) {
	rows, err := tx.QueryContext(ctx,
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

// listApprovalsBatch fetches all approvals for the given set of
// promotion IDs in a single query and returns them grouped by
// promotion ID (DA-H3 fix). Never called with an empty ids slice.
func (s *PromotionStore) listApprovalsBatch(ctx context.Context, ids []string) (map[string][]model.PromotionApproval, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, promotion_id, approver_sub, approver_email, note, created_at
		   FROM promotion_approvals
		  WHERE promotion_id = ANY($1)
		  ORDER BY promotion_id, created_at ASC`,
		pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("batch listing approvals: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]model.PromotionApproval, len(ids))
	for rows.Next() {
		var a model.PromotionApproval
		if err := rows.Scan(&a.ID, &a.PromotionID, &a.ApproverSub, &a.ApproverEmail, &a.Note, &a.CreatedAt); err != nil {
			return nil, err
		}
		out[a.PromotionID] = append(out[a.PromotionID], a)
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
