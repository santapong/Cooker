package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// StageApprovalService persists per-stage approval gates and their votes
// (audit finding HS26-05-03). It is the authority both the executor (to
// open and poll a gate) and the HTTP handler (to approve/reject) route
// through; neither touches the store for gate logic directly. Mirrors
// PromotionService. See docs/adr/0005-promotion-approval-persistence.md.
type StageApprovalService struct {
	gates store.StageApprovalStore
}

// NewStageApprovalService wires the gate service to its store.
func NewStageApprovalService(gates store.StageApprovalStore) *StageApprovalService {
	return &StageApprovalService{gates: gates}
}

// Approve reuses the package-level ErrAlreadyApproved sentinel (defined in
// promoter.go) for the "same approver voted twice" case, so handlers have a
// single idempotency signal across promotion and stage approvals.

// ErrGateResolved is returned by Approve/Reject when the gate has already
// settled (approved or rejected). The handler maps it to 409 Conflict so a
// late click on a resolved gate is reported honestly rather than silently
// re-stamping the row.
var ErrGateResolved = errors.New("stageapproval: gate already resolved")

// Await returns the existing gate for (run, stage) or creates a new one in
// the awaiting state. requiredApprovers is snapshotted onto a freshly
// created gate (normalised to >= 1). Idempotent per (run, stage): a stage
// re-entered after a restart finds its existing gate instead of resetting
// the approval count.
func (s *StageApprovalService) Await(ctx context.Context, runID, stageID string, requiredApprovers int) (*model.StageApproval, error) {
	if existing, err := s.gates.GetGate(ctx, runID, stageID); err == nil {
		return s.recoverQuorum(ctx, existing)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("await: check existing gate: %w", err)
	}

	g := &model.StageApproval{
		ID:                uuid.NewString(),
		RunID:             runID,
		StageID:           stageID,
		Status:            model.StageApprovalAwaiting,
		RequiredApprovers: normalizeApprovers(requiredApprovers),
	}
	if err := s.gates.CreateGate(ctx, g); err != nil {
		if errors.Is(err, store.ErrConflict) {
			// Lost a race; return the winner.
			if existing, gerr := s.gates.GetGate(ctx, runID, stageID); gerr == nil {
				return s.recoverQuorum(ctx, existing)
			}
		}
		return nil, fmt.Errorf("await: persist gate: %w", err)
	}
	slog.Info("stage approval gate opened", "run", runID, "stage", stageID,
		"requiredApprovers", g.RequiredApprovers)
	return g, nil
}

// Get returns the current gate for (run, stage). The executor polls this
// while blocked; ErrNotFound means no gate exists yet.
func (s *StageApprovalService) Get(ctx context.Context, runID, stageID string) (*model.StageApproval, error) {
	gate, err := s.gates.GetGate(ctx, runID, stageID)
	if err != nil {
		return nil, err
	}
	return s.recoverQuorum(ctx, gate)
}

// Recover gates written by the earlier split vote/resolution implementation.
// An executor re-attaching or polling must not wait forever on committed quorum.
// The original resolver is unknown; record the recovery time without inventing
// an approver. The await-only store fence preserves any concurrent veto/winner.
func (s *StageApprovalService) recoverQuorum(ctx context.Context, gate *model.StageApproval) (*model.StageApproval, error) {
	if gate.Status != model.StageApprovalAwaiting || gate.ApprovalCount() < normalizeApprovers(gate.RequiredApprovers) {
		return gate, nil
	}
	now := time.Now()
	if err := s.gates.UpdateGateStatus(ctx, gate.ID, model.StageApprovalApproved, "", &now); err != nil {
		if errors.Is(err, store.ErrConflict) {
			winner, readErr := s.gates.GetGate(ctx, gate.RunID, gate.StageID)
			if readErr != nil {
				return nil, fmt.Errorf("recover stage approval: reload winner: %w", readErr)
			}
			if winner.Status != model.StageApprovalAwaiting {
				return winner, nil
			}
		}
		return nil, fmt.Errorf("recover stage approval quorum: %w", err)
	}
	// Return the persisted observation (including database timestamp precision),
	// not a locally reconstructed status/time after the conditional write.
	resolved, err := s.gates.GetGate(ctx, gate.RunID, gate.StageID)
	if err != nil {
		return nil, fmt.Errorf("recover stage approval: reload result: %w", err)
	}
	slog.Info("stage approval quorum recovered", "run", gate.RunID, "stage", gate.StageID,
		"approvals", gate.ApprovalCount(), "required", gate.RequiredApprovers)
	return resolved, nil
}

// List returns all approval gates for a run (awaiting + resolved), each
// with its votes populated. Backs the run page's gate poll so it can show
// an Approve/Reject affordance on awaiting stages. A run with no approval
// stages returns an empty (non-nil) slice.
func (s *StageApprovalService) List(ctx context.Context, runID string) ([]*model.StageApproval, error) {
	gates, err := s.gates.ListGates(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list gates: %w", err)
	}
	if gates == nil {
		gates = []*model.StageApproval{}
	}
	return gates, nil
}

// Approve records an approval vote by the given approver, then advances
// the gate to "approved" once the distinct-approver count reaches
// RequiredApprovers. The approver identity comes from the caller's OIDC
// claims, never the request body. Returns the updated gate; if this
// approver had already voted, returns the gate together with
// ErrAlreadyApproved so the handler can respond idempotently. Returns
// ErrGateResolved if the gate has already settled.
func (s *StageApprovalService) Approve(ctx context.Context, runID, stageID, approverSub, approverEmail, note string) (*model.StageApproval, error) {
	g, err := s.Get(ctx, runID, stageID)
	if err != nil {
		return nil, fmt.Errorf("approve stage: load gate: %w", err)
	}
	if g.Status != model.StageApprovalAwaiting {
		return g, ErrGateResolved
	}

	added, _, err := s.gates.AddVote(ctx, &model.StageApprovalVote{
		ID:              uuid.NewString(),
		StageApprovalID: g.ID,
		ApproverSub:     approverSub,
		ApproverEmail:   approverEmail,
		Note:            note,
	})
	if err != nil {
		return s.resolveDecisionConflict(ctx, runID, stageID, fmt.Errorf("approve stage: record vote: %w", err))
	}

	// Re-read so the returned gate carries the full vote set.
	g, err = s.gates.GetGate(ctx, runID, stageID)
	if err != nil {
		return nil, fmt.Errorf("approve stage: reload gate: %w", err)
	}
	if g.Status != model.StageApprovalAwaiting && g.Status != model.StageApprovalApproved {
		return g, ErrGateResolved
	}

	// AddVote commits quorum resolution with the vote. This read supplies the
	// response only; interruption here cannot strand a committed final vote.
	if g.Status == model.StageApprovalApproved {
		slog.Info("stage approval gate approved", "run", runID, "stage", stageID,
			"approvals", g.ApprovalCount(), "required", g.RequiredApprovers)
	}

	if !added {
		return g, ErrAlreadyApproved
	}
	return g, nil
}

// Reject settles the gate as "rejected" — a single rejection short-
// circuits the gate regardless of RequiredApprovers (one veto is enough).
// The executor then fails the stage. Returns ErrGateResolved if the gate
// has already settled.
func (s *StageApprovalService) Reject(ctx context.Context, runID, stageID, approverEmail, note string) (*model.StageApproval, error) {
	g, err := s.Get(ctx, runID, stageID)
	if err != nil {
		return nil, fmt.Errorf("reject stage: load gate: %w", err)
	}
	if g.Status != model.StageApprovalAwaiting {
		return g, ErrGateResolved
	}
	now := time.Now()
	if err := s.gates.UpdateGateStatus(ctx, g.ID, model.StageApprovalRejected, approverEmail, &now); err != nil {
		return s.resolveDecisionConflict(ctx, runID, stageID, fmt.Errorf("reject stage: settle gate: %w", err))
	}
	resolved, err := s.gates.GetGate(ctx, runID, stageID)
	if err != nil {
		return nil, fmt.Errorf("reject stage: reload result: %w", err)
	}
	slog.Info("stage approval gate rejected", "run", runID, "stage", stageID, "by", approverEmail)
	return resolved, nil
}

// A stale awaiting read cannot authorize replacement of the winning terminal
// decision. Return that recorded decision so the handler can report an honest
// conflict, rather than a 500 or a locally invented success state.
func (s *StageApprovalService) resolveDecisionConflict(ctx context.Context, runID, stageID string, cause error) (*model.StageApproval, error) {
	if errors.Is(cause, store.ErrConflict) {
		gate, err := s.gates.GetGate(ctx, runID, stageID)
		if err != nil {
			return nil, fmt.Errorf("reload resolved stage approval: %w", err)
		}
		if gate.Status != model.StageApprovalAwaiting {
			return gate, ErrGateResolved
		}
	}
	return nil, cause
}

// normalizeApprovers treats a non-positive threshold as 1, so a
// misconfigured gate can't complete with zero approvers.
func normalizeApprovers(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
