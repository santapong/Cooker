package service

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

func newStageApprovalService() *StageApprovalService {
	return NewStageApprovalService(memory.New().StageApprovals)
}

func TestStageApprovalService_AwaitIsIdempotent(t *testing.T) {
	svc := newStageApprovalService()
	ctx := context.Background()

	g1, err := svc.Await(ctx, "run-1", "stage-1", 2)
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	if g1.Status != model.StageApprovalAwaiting {
		t.Errorf("status = %s, want awaiting", g1.Status)
	}
	if g1.RequiredApprovers != 2 {
		t.Errorf("requiredApprovers = %d, want 2", g1.RequiredApprovers)
	}

	// A second Await for the same (run, stage) returns the existing gate,
	// not a fresh one with a reset count.
	g2, err := svc.Await(ctx, "run-1", "stage-1", 5)
	if err != nil {
		t.Fatalf("await again: %v", err)
	}
	if g2.ID != g1.ID {
		t.Errorf("await returned a new gate %s, want existing %s", g2.ID, g1.ID)
	}
	if g2.RequiredApprovers != 2 {
		t.Errorf("requiredApprovers changed to %d on re-await, want 2", g2.RequiredApprovers)
	}
}

func TestStageApprovalService_AwaitDefaultsToOneApprover(t *testing.T) {
	svc := newStageApprovalService()
	g, err := svc.Await(context.Background(), "run-1", "stage-1", 0)
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	if g.RequiredApprovers != 1 {
		t.Errorf("requiredApprovers = %d, want 1 (default)", g.RequiredApprovers)
	}
}

func TestStageApprovalService_ApproveAdvancesAtThreshold(t *testing.T) {
	svc := newStageApprovalService()
	ctx := context.Background()
	if _, err := svc.Await(ctx, "run-1", "stage-1", 2); err != nil {
		t.Fatalf("await: %v", err)
	}

	// First approver: still awaiting (need 2).
	g, err := svc.Approve(ctx, "run-1", "stage-1", "sub-a", "a@x.com", "")
	if err != nil {
		t.Fatalf("approve a: %v", err)
	}
	if g.Status != model.StageApprovalAwaiting {
		t.Errorf("status after 1/2 = %s, want awaiting", g.Status)
	}

	// Second, distinct approver: threshold met, gate approved.
	g, err = svc.Approve(ctx, "run-1", "stage-1", "sub-b", "b@x.com", "")
	if err != nil {
		t.Fatalf("approve b: %v", err)
	}
	if g.Status != model.StageApprovalApproved {
		t.Errorf("status after 2/2 = %s, want approved", g.Status)
	}
	if g.ResolvedAt == nil {
		t.Error("approved gate should stamp ResolvedAt")
	}
}

func TestStageApprovalService_ApproveIsIdempotentPerApprover(t *testing.T) {
	svc := newStageApprovalService()
	ctx := context.Background()
	if _, err := svc.Await(ctx, "run-1", "stage-1", 2); err != nil {
		t.Fatalf("await: %v", err)
	}

	if _, err := svc.Approve(ctx, "run-1", "stage-1", "sub-a", "a@x.com", ""); err != nil {
		t.Fatalf("approve a: %v", err)
	}
	// Same approver again: ErrAlreadyApproved, count unchanged, still awaiting.
	g, err := svc.Approve(ctx, "run-1", "stage-1", "sub-a", "a@x.com", "")
	if !errors.Is(err, ErrAlreadyApproved) {
		t.Fatalf("re-approve err = %v, want ErrAlreadyApproved", err)
	}
	if g.ApprovalCount() != 1 {
		t.Errorf("approval count = %d, want 1 (double-click counts once)", g.ApprovalCount())
	}
	if g.Status != model.StageApprovalAwaiting {
		t.Errorf("status = %s, want still awaiting", g.Status)
	}
}

func TestStageApprovalService_RejectSettlesGate(t *testing.T) {
	svc := newStageApprovalService()
	ctx := context.Background()
	if _, err := svc.Await(ctx, "run-1", "stage-1", 3); err != nil {
		t.Fatalf("await: %v", err)
	}

	// One rejection settles the gate regardless of RequiredApprovers.
	g, err := svc.Reject(ctx, "run-1", "stage-1", "rej@x.com", "no go")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if g.Status != model.StageApprovalRejected {
		t.Errorf("status = %s, want rejected", g.Status)
	}
	if g.ResolvedBy != "rej@x.com" {
		t.Errorf("resolvedBy = %q, want rej@x.com", g.ResolvedBy)
	}
}

func TestStageApprovalService_ApproveAfterResolvedConflicts(t *testing.T) {
	svc := newStageApprovalService()
	ctx := context.Background()
	if _, err := svc.Await(ctx, "run-1", "stage-1", 1); err != nil {
		t.Fatalf("await: %v", err)
	}
	if _, err := svc.Reject(ctx, "run-1", "stage-1", "rej@x.com", ""); err != nil {
		t.Fatalf("reject: %v", err)
	}

	// Approving a rejected gate is a conflict, not a silent re-open.
	_, err := svc.Approve(ctx, "run-1", "stage-1", "sub-a", "a@x.com", "")
	if !errors.Is(err, ErrGateResolved) {
		t.Fatalf("approve-after-reject err = %v, want ErrGateResolved", err)
	}
}

func TestStageApprovalService_ListReturnsGates(t *testing.T) {
	svc := newStageApprovalService()
	ctx := context.Background()
	if _, err := svc.Await(ctx, "run-1", "stage-1", 1); err != nil {
		t.Fatalf("await s1: %v", err)
	}
	if _, err := svc.Await(ctx, "run-1", "stage-2", 1); err != nil {
		t.Fatalf("await s2: %v", err)
	}

	gates, err := svc.List(ctx, "run-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(gates) != 2 {
		t.Errorf("listed %d gates, want 2", len(gates))
	}

	// A run with no approval stages returns an empty, non-nil slice.
	none, err := svc.List(ctx, "run-none")
	if err != nil {
		t.Fatalf("list none: %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Errorf("list for run with no gates = %v, want empty non-nil slice", none)
	}
}
