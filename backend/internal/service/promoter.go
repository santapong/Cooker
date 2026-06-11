package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// Promoter handles promotion logic between environments (Dev → Staging → Production).
type Promoter struct {
	environments []*model.Environment
}

// NewPromoter creates a promoter with the given environments.
func NewPromoter(envs []*model.Environment) *Promoter {
	sorted := make([]*model.Environment, len(envs))
	copy(sorted, envs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Order < sorted[j].Order
	})
	return &Promoter{environments: sorted}
}

// NextEnvironment returns the next environment in the promotion chain, or nil if at the end.
func (p *Promoter) NextEnvironment(currentEnvID string) *model.Environment {
	for i, env := range p.environments {
		if env.ID == currentEnvID && i+1 < len(p.environments) {
			return p.environments[i+1]
		}
	}
	return nil
}

// ShouldAutoPromote checks if the next environment allows automatic promotion.
func (p *Promoter) ShouldAutoPromote(nextEnv *model.Environment) bool {
	return nextEnv.Promotion.Strategy == "auto"
}

// Promote attempts to promote a pipeline run to the next environment.
func (p *Promoter) Promote(run *model.PipelineRun, currentEnvID string, approvedBy string) (*model.EnvironmentStatus, error) {
	nextEnv := p.NextEnvironment(currentEnvID)
	if nextEnv == nil {
		return nil, fmt.Errorf("no next environment to promote to")
	}

	if nextEnv.Promotion.Strategy == "manual" && approvedBy == "" {
		// Set to awaiting approval
		status := &model.EnvironmentStatus{
			EnvironmentID: nextEnv.ID,
			Status:        model.EnvStatusAwaitingApproval,
		}
		run.EnvironmentStatuses = append(run.EnvironmentStatuses, *status)
		slog.Info("promoter awaiting approval", "run", run.ID, "env", nextEnv.Name)
		return status, nil
	}

	// Promote
	now := time.Now()
	status := &model.EnvironmentStatus{
		EnvironmentID: nextEnv.ID,
		Status:        model.EnvStatusDeploying,
		PromotedAt:    &now,
		ApprovedBy:    approvedBy,
	}
	run.EnvironmentStatuses = append(run.EnvironmentStatuses, *status)

	slog.Info("promoter promoted run", "run", run.ID, "env", nextEnv.Name, "approvedBy", approvedBy)
	return status, nil
}

// ApprovePromotion approves a pending promotion and triggers deployment.
func (p *Promoter) ApprovePromotion(run *model.PipelineRun, envID string, approvedBy string) error {
	for i := range run.EnvironmentStatuses {
		es := &run.EnvironmentStatuses[i]
		if es.EnvironmentID == envID && es.Status == model.EnvStatusAwaitingApproval {
			now := time.Now()
			es.Status = model.EnvStatusDeploying
			es.PromotedAt = &now
			es.ApprovedBy = approvedBy
			slog.Info("promotion approved", "env", envID, "approvedBy", approvedBy)
			return nil
		}
	}
	return fmt.Errorf("no pending promotion found for environment %s", envID)
}

// --- Persistent promotion/approval flow (HS26-05-01 / -08 / -14) ---
//
// The methods above operate on the in-memory run.EnvironmentStatuses
// slice and back the executor's auto-promote chain. The methods below
// persist promotions and approvals as rows so the flow survives a
// restart, enforces PromotionPolicy.RequiredApprovers with a DB-level
// distinct-approver guarantee, and feeds GetEnvStatus real data.
// See docs/adr/0005-promotion-approval-persistence.md.

// promotionEnvStore is the narrow slice of EnvironmentStore the
// persistent promoter needs. Defined here (not imported wholesale) so
// the service depends only on what it uses and tests can fake it.
type promotionEnvStore interface {
	Get(ctx context.Context, id string) (*model.Environment, error)
}

// PromotionService persists run→environment promotions and their
// approvals. It is the authority the HTTP handler routes through; the
// handler stays thin (parse + authorize) and never touches the store
// for promotion logic directly.
type PromotionService struct {
	promotions store.PromotionStore
	envs       promotionEnvStore
}

// NewPromotionService wires the persistent promoter to its stores.
func NewPromotionService(promotions store.PromotionStore, envs promotionEnvStore) *PromotionService {
	return &PromotionService{promotions: promotions, envs: envs}
}

// ErrAlreadyApproved is returned by Approve when the same approver has
// already approved the gate. The handler maps it to a benign 200 (the
// desired end state already holds) rather than an error.
var ErrAlreadyApproved = errors.New("promoter: already approved by this user")

// Promote records (or returns the existing) promotion of run → the
// target environment. The target's PromotionPolicy decides the outcome:
//   - strategy "auto": the promotion is created terminal (deployed) with
//     no approval required.
//   - strategy "manual": the promotion is created awaiting_approval; a
//     RequiredApprovers count is snapshotted from the policy.
//
// Promote is idempotent per (run, environment): a second call returns
// the existing promotion instead of erroring, so a double-click in the
// UI is harmless.
func (s *PromotionService) Promote(ctx context.Context, runID, pipelineID, environmentID, requestedBy string) (*model.RunPromotion, error) {
	env, err := s.envs.Get(ctx, environmentID)
	if err != nil {
		return nil, fmt.Errorf("promote: load target env: %w", err)
	}

	// Idempotency: if a promotion already exists for this target, return
	// it rather than creating a duplicate (the UNIQUE constraint would
	// reject the insert anyway).
	if existing, err := s.promotions.GetPromotion(ctx, runID, environmentID); err == nil {
		return existing, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("promote: check existing: %w", err)
	}

	now := time.Now()
	p := &model.RunPromotion{
		ID:                uuid.NewString(),
		RunID:             runID,
		PipelineID:        pipelineID,
		EnvironmentID:     environmentID,
		Strategy:          env.Promotion.Strategy,
		RequiredApprovers: requiredApprovers(env.Promotion),
		RequestedBy:       requestedBy,
	}

	if env.Promotion.Strategy == "auto" {
		// Auto short-circuits: no gate, immediately terminal.
		p.Status = model.PromotionDeployed
		p.PromotedAt = &now
		p.RequiredApprovers = 0
	} else {
		p.Status = model.PromotionAwaitingApproval
	}

	if err := s.promotions.CreatePromotion(ctx, p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			// Lost a race with a concurrent promote; return the winner.
			if existing, gerr := s.promotions.GetPromotion(ctx, runID, environmentID); gerr == nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("promote: persist: %w", err)
	}
	slog.Info("promotion created", "run", runID, "env", environmentID,
		"strategy", p.Strategy, "status", p.Status, "requiredApprovers", p.RequiredApprovers)
	return p, nil
}

// Approve records an approval for a run's promotion to environmentID by
// the given approver, then advances the gate if the distinct-approver
// count has reached RequiredApprovers. The approver identity comes from
// the caller's OIDC claims, never the request body.
//
// Returns the updated promotion. If this approver had already approved,
// returns the unchanged promotion together with ErrAlreadyApproved so
// the handler can respond idempotently.
func (s *PromotionService) Approve(ctx context.Context, runID, environmentID, approverSub, approverEmail, note string) (*model.RunPromotion, error) {
	p, err := s.promotions.GetPromotion(ctx, runID, environmentID)
	if err != nil {
		return nil, fmt.Errorf("approve: load promotion: %w", err)
	}

	added, count, err := s.promotions.AddApproval(ctx, &model.PromotionApproval{
		ID:            uuid.NewString(),
		PromotionID:   p.ID,
		ApproverSub:   approverSub,
		ApproverEmail: approverEmail,
		Note:          note,
	})
	if err != nil {
		return nil, fmt.Errorf("approve: record approval: %w", err)
	}

	// Re-read so the returned promotion carries the full approval set.
	p, err = s.promotions.GetPromotion(ctx, runID, environmentID)
	if err != nil {
		return nil, fmt.Errorf("approve: reload promotion: %w", err)
	}

	// Advance the gate once the threshold is met (only from a non-terminal
	// awaiting state, so a late duplicate approval can't re-stamp a row).
	if count >= p.RequiredApprovers && p.Status == model.PromotionAwaitingApproval {
		now := time.Now()
		if err := s.promotions.UpdatePromotionStatus(ctx, p.ID, model.PromotionApproved, &now); err != nil {
			return nil, fmt.Errorf("approve: advance gate: %w", err)
		}
		p.Status = model.PromotionApproved
		p.PromotedAt = &now
		slog.Info("promotion approved", "run", runID, "env", environmentID,
			"approvals", count, "required", p.RequiredApprovers)
	}

	if !added {
		return p, ErrAlreadyApproved
	}
	return p, nil
}

// Statuses returns the per-environment promotion status for a run,
// projected onto the EnvironmentStatus shape the run page consumes.
// A run with no promotions yet returns an empty (non-nil) slice.
func (s *PromotionService) Statuses(ctx context.Context, runID string) ([]model.EnvironmentStatus, error) {
	promos, err := s.promotions.ListPromotions(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("env-status: list promotions: %w", err)
	}
	out := make([]model.EnvironmentStatus, 0, len(promos))
	for _, p := range promos {
		out = append(out, p.ToEnvironmentStatus())
	}
	return out, nil
}

// requiredApprovers normalises the policy's threshold for a manual gate.
// A manual gate with RequiredApprovers <= 0 is treated as needing one
// approval, so a misconfigured policy can't complete with zero approvers.
func requiredApprovers(pol model.PromotionPolicy) int {
	if pol.Strategy == "auto" {
		return 0
	}
	if pol.RequiredApprovers < 1 {
		return 1
	}
	return pol.RequiredApprovers
}
