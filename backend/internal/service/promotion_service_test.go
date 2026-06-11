package service

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

// fakeEnvStore lets a test pin a target environment's PromotionPolicy
// without standing up the full EnvironmentStore.
type fakeEnvStore struct{ envs map[string]*model.Environment }

func (f *fakeEnvStore) Get(_ context.Context, id string) (*model.Environment, error) {
	e, ok := f.envs[id]
	if !ok {
		return nil, errors.New("env not found")
	}
	return e, nil
}

func newPromotionService(envs ...*model.Environment) *PromotionService {
	m := make(map[string]*model.Environment, len(envs))
	for _, e := range envs {
		m[e.ID] = e
	}
	st := memory.New()
	return NewPromotionService(st.Promotions, &fakeEnvStore{envs: m})
}

func TestPromotionService_AutoStrategyShortCircuits(t *testing.T) {
	svc := newPromotionService(&model.Environment{
		ID: "dev", Promotion: model.PromotionPolicy{Strategy: "auto"},
	})
	ctx := context.Background()

	p, err := svc.Promote(ctx, "run-1", "pl-1", "dev", "op@x.com")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if p.Status != model.PromotionDeployed {
		t.Errorf("auto promotion should be deployed immediately, got %s", p.Status)
	}
	if p.PromotedAt == nil {
		t.Error("auto promotion should stamp PromotedAt")
	}
	if p.RequiredApprovers != 0 {
		t.Errorf("auto promotion needs no approvers, got %d", p.RequiredApprovers)
	}
}

func TestPromotionService_ManualWaitsForApproval(t *testing.T) {
	svc := newPromotionService(&model.Environment{
		ID: "staging", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 1},
	})
	ctx := context.Background()

	p, err := svc.Promote(ctx, "run-1", "pl-1", "staging", "op@x.com")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if p.Status != model.PromotionAwaitingApproval {
		t.Fatalf("manual promotion should await approval, got %s", p.Status)
	}

	got, err := svc.Approve(ctx, "run-1", "staging", "sub-alice", "alice@x.com", "lgtm")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if got.Status != model.PromotionApproved {
		t.Errorf("one approval meets threshold 1, expected approved, got %s", got.Status)
	}
}

func TestPromotionService_RequiresDistinctApprovers(t *testing.T) {
	svc := newPromotionService(&model.Environment{
		ID: "prod", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 2},
	})
	ctx := context.Background()
	if _, err := svc.Promote(ctx, "run-1", "pl-1", "prod", "op@x.com"); err != nil {
		t.Fatal(err)
	}

	// First approver: still awaiting (1 of 2).
	p, err := svc.Approve(ctx, "run-1", "prod", "sub-alice", "alice@x.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != model.PromotionAwaitingApproval {
		t.Fatalf("1 of 2 approvals should still await, got %s", p.Status)
	}

	// Same approver again: idempotent, ErrAlreadyApproved, still 1 of 2.
	p, err = svc.Approve(ctx, "run-1", "prod", "sub-alice", "alice@x.com", "")
	if !errors.Is(err, ErrAlreadyApproved) {
		t.Fatalf("re-approval by same user should report ErrAlreadyApproved, got %v", err)
	}
	if p.Status != model.PromotionAwaitingApproval {
		t.Errorf("duplicate approval must not advance the gate, status=%s", p.Status)
	}
	if p.ApprovalCount() != 1 {
		t.Errorf("duplicate approval must not raise the count, got %d", p.ApprovalCount())
	}

	// Second DISTINCT approver: threshold met.
	p, err = svc.Approve(ctx, "run-1", "prod", "sub-bob", "bob@x.com", "ship it")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != model.PromotionApproved {
		t.Errorf("2 distinct approvals should meet threshold 2, got %s", p.Status)
	}
	if p.ApprovalCount() != 2 {
		t.Errorf("expected 2 approvals, got %d", p.ApprovalCount())
	}
}

func TestPromotionService_ManualZeroApproversTreatedAsOne(t *testing.T) {
	// A manual gate misconfigured with RequiredApprovers=0 must NOT
	// auto-complete with zero approvals — it needs one.
	svc := newPromotionService(&model.Environment{
		ID: "staging", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 0},
	})
	ctx := context.Background()
	p, err := svc.Promote(ctx, "run-1", "pl-1", "staging", "op@x.com")
	if err != nil {
		t.Fatal(err)
	}
	if p.RequiredApprovers != 1 {
		t.Errorf("manual gate with 0 approvers should normalise to 1, got %d", p.RequiredApprovers)
	}
	if p.Status != model.PromotionAwaitingApproval {
		t.Errorf("should still await one approval, got %s", p.Status)
	}
}

func TestPromotionService_PromoteIdempotent(t *testing.T) {
	svc := newPromotionService(&model.Environment{
		ID: "staging", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 1},
	})
	ctx := context.Background()
	p1, err := svc.Promote(ctx, "run-1", "pl-1", "staging", "op@x.com")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := svc.Promote(ctx, "run-1", "pl-1", "staging", "op@x.com")
	if err != nil {
		t.Fatalf("second promote should be idempotent, got %v", err)
	}
	if p1.ID != p2.ID {
		t.Errorf("idempotent promote should return the same promotion: %s vs %s", p1.ID, p2.ID)
	}
}

func TestPromotionService_StatusesReflectRows(t *testing.T) {
	svc := newPromotionService(
		&model.Environment{ID: "dev", Promotion: model.PromotionPolicy{Strategy: "auto"}},
		&model.Environment{ID: "staging", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 2}},
	)
	ctx := context.Background()

	// No promotions yet → empty, non-nil slice.
	statuses, err := svc.Statuses(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if statuses == nil {
		t.Fatal("Statuses should return a non-nil slice")
	}
	if len(statuses) != 0 {
		t.Fatalf("expected 0 statuses before any promotion, got %d", len(statuses))
	}

	if _, err := svc.Promote(ctx, "run-1", "pl-1", "dev", "op@x.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Promote(ctx, "run-1", "pl-1", "staging", "op@x.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(ctx, "run-1", "staging", "sub-alice", "alice@x.com", ""); err != nil {
		t.Fatal(err)
	}

	statuses, err = svc.Statuses(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 env statuses, got %d", len(statuses))
	}
	byEnv := map[string]model.EnvironmentStatus{}
	for _, s := range statuses {
		byEnv[s.EnvironmentID] = s
	}
	if byEnv["dev"].Status != model.EnvStatusDeployed {
		t.Errorf("dev (auto) should be deployed, got %s", byEnv["dev"].Status)
	}
	staging := byEnv["staging"]
	if staging.Status != model.EnvStatus(model.PromotionAwaitingApproval) {
		t.Errorf("staging should await approval, got %s", staging.Status)
	}
	if staging.ApprovalsHave != 1 || staging.ApprovalsNeed != 2 {
		t.Errorf("staging gate progress = %d/%d, want 1/2", staging.ApprovalsHave, staging.ApprovalsNeed)
	}
}

func TestPromotionService_ApproveUnknownPromotion(t *testing.T) {
	svc := newPromotionService(&model.Environment{
		ID: "staging", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 1},
	})
	// No promote first → no promotion row.
	if _, err := svc.Approve(context.Background(), "run-x", "staging", "sub", "e@x.com", ""); err == nil {
		t.Fatal("approve without a promotion should error")
	}
}
