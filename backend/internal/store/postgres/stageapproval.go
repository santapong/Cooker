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

// StageApprovalStore implements store.StageApprovalStore using PostgreSQL.
// Gates and their votes live in the stage_approvals and
// stage_approval_votes tables (migration 022). Mirrors PromotionStore.
type StageApprovalStore struct {
	db *sql.DB
}

// NewStageApprovalStore creates a PostgreSQL-backed stage-approval store.
func NewStageApprovalStore(db *sql.DB) *StageApprovalStore {
	return &StageApprovalStore{db: db}
}

func (s *StageApprovalStore) CreateGate(ctx context.Context, g *model.StageApproval) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO stage_approvals
		   (id, run_id, stage_id, status, required_approvers, resolved_by, resolved_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING created_at, updated_at`,
		g.ID, g.RunID, g.StageID, string(g.Status), g.RequiredApprovers,
		g.ResolvedBy, nullTime(g.ResolvedAt)).
		Scan(&g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		var pqErr *pq.Error
		// 23505 = unique_violation: a gate already exists for this
		// (run, stage). Map to ErrConflict so gate creation is idempotent.
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return fmt.Errorf("stage approval %s/%s: %w", g.RunID, g.StageID, store.ErrConflict)
		}
		return fmt.Errorf("creating stage approval: %w", err)
	}
	return nil
}

// GetGate fetches a gate and its votes inside a single read transaction
// (TOCTOU fix, DA-M): the gate row and the vote rows are read from the
// same consistent snapshot, so a concurrent AddVote cannot produce a
// count that disagrees with the vote list.
func (s *StageApprovalStore) GetGate(ctx context.Context, runID, stageID string) (*model.StageApproval, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("getting gate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // read-only rollback is always safe

	row := tx.QueryRowContext(ctx,
		`SELECT id, run_id, stage_id, status, required_approvers, resolved_by,
		        resolved_at, created_at, updated_at
		   FROM stage_approvals WHERE run_id = $1 AND stage_id = $2`,
		runID, stageID)
	g, err := scanStageApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("stage approval %s/%s: %w", runID, stageID, store.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	votes, err := listVotesTx(ctx, tx, g.ID)
	if err != nil {
		return nil, err
	}
	g.Votes = votes
	return g, nil
}

// ListGates returns all gates for a run with their votes. Votes are
// fetched in a single batched query (DA-H3 fix) rather than one query
// per gate, to avoid the N+1 pattern.
func (s *StageApprovalStore) ListGates(ctx context.Context, runID string) ([]*model.StageApproval, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, stage_id, status, required_approvers, resolved_by,
		        resolved_at, created_at, updated_at
		   FROM stage_approvals WHERE run_id = $1
		  ORDER BY created_at ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("listing stage approvals: %w", err)
	}
	defer rows.Close()

	var out []*model.StageApproval
	var ids []string
	for rows.Next() {
		g, err := scanStageApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
		ids = append(ids, g.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}

	// Fetch all votes for this run's gates in one query (DA-H3).
	voteMap, err := s.listVotesBatch(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, g := range out {
		g.Votes = voteMap[g.ID]
	}
	return out, nil
}

func (s *StageApprovalStore) UpdateGateStatus(ctx context.Context, id string, status model.StageApprovalStatus, resolvedBy string, resolvedAt *time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE stage_approvals
		    SET status = $2,
		        resolved_by = CASE WHEN $3 = '' THEN resolved_by ELSE $3 END,
		        resolved_at = COALESCE($4, resolved_at),
		        updated_at = NOW()
		  WHERE id = $1`,
		id, string(status), resolvedBy, nullTime(resolvedAt))
	if err != nil {
		return fmt.Errorf("updating stage approval %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("stage approval %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// AddVote inserts a vote row and returns the post-insert count. Both
// statements execute inside a single transaction (DA-H2 fix) so
// concurrent votes cannot observe a count that omits a just-inserted
// peer row, which would cause a gate to stall permanently.
func (s *StageApprovalStore) AddVote(ctx context.Context, v *model.StageApprovalVote) (bool, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, fmt.Errorf("adding vote: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// ON CONFLICT DO NOTHING makes a repeat approval by the same identity
	// a no-op rather than a unique-violation. RETURNING id fires only when
	// a row was inserted; a missing scan row means "already approved".
	var insertedID string
	err = tx.QueryRowContext(ctx,
		`INSERT INTO stage_approval_votes
		   (id, stage_approval_id, approver_sub, approver_email, note)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (stage_approval_id, approver_sub) DO NOTHING
		 RETURNING id`,
		v.ID, v.StageApprovalID, v.ApproverSub, v.ApproverEmail, v.Note).Scan(&insertedID)
	added := true
	if errors.Is(err, sql.ErrNoRows) {
		added = false
	} else if err != nil {
		var pqErr *pq.Error
		// 23503 = foreign_key_violation: the gate is gone.
		if errors.As(err, &pqErr) && pqErr.Code == "23503" {
			return false, 0, fmt.Errorf("stage approval %s: %w", v.StageApprovalID, store.ErrNotFound)
		}
		return false, 0, fmt.Errorf("adding stage approval vote: %w", err)
	}

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM stage_approval_votes WHERE stage_approval_id = $1`,
		v.StageApprovalID).Scan(&count); err != nil {
		return false, 0, fmt.Errorf("counting stage approval votes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, 0, fmt.Errorf("adding vote: commit: %w", err)
	}
	return added, count, nil
}

// listVotesTx fetches votes for a single gate within the supplied
// transaction (used by GetGate's TOCTOU-safe read tx).
func listVotesTx(ctx context.Context, tx *sql.Tx, gateID string) ([]model.StageApprovalVote, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, stage_approval_id, approver_sub, approver_email, note, created_at
		   FROM stage_approval_votes WHERE stage_approval_id = $1
		  ORDER BY created_at ASC`, gateID)
	if err != nil {
		return nil, fmt.Errorf("listing stage approval votes: %w", err)
	}
	defer rows.Close()

	var out []model.StageApprovalVote
	for rows.Next() {
		var v model.StageApprovalVote
		if err := rows.Scan(&v.ID, &v.StageApprovalID, &v.ApproverSub, &v.ApproverEmail, &v.Note, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// listVotesBatch fetches all votes for the given set of gate IDs in a
// single query and returns them grouped by gate ID (DA-H3 fix). Never
// called with an empty ids slice.
func (s *StageApprovalStore) listVotesBatch(ctx context.Context, ids []string) (map[string][]model.StageApprovalVote, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, stage_approval_id, approver_sub, approver_email, note, created_at
		   FROM stage_approval_votes
		  WHERE stage_approval_id = ANY($1)
		  ORDER BY stage_approval_id, created_at ASC`,
		pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("batch listing stage approval votes: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]model.StageApprovalVote, len(ids))
	for rows.Next() {
		var v model.StageApprovalVote
		if err := rows.Scan(&v.ID, &v.StageApprovalID, &v.ApproverSub, &v.ApproverEmail, &v.Note, &v.CreatedAt); err != nil {
			return nil, err
		}
		out[v.StageApprovalID] = append(out[v.StageApprovalID], v)
	}
	return out, rows.Err()
}

func scanStageApproval(row scannable) (*model.StageApproval, error) {
	g := &model.StageApproval{}
	var status string
	var resolvedAt sql.NullTime
	if err := row.Scan(&g.ID, &g.RunID, &g.StageID, &status, &g.RequiredApprovers,
		&g.ResolvedBy, &resolvedAt, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	g.Status = model.StageApprovalStatus(status)
	if resolvedAt.Valid {
		t := resolvedAt.Time
		g.ResolvedAt = &t
	}
	return g, nil
}
