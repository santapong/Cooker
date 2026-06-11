package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestPromotionStore_CreateGetListParity(t *testing.T) {
	st := New()
	ctx := context.Background()

	p := &model.RunPromotion{
		ID: "promo-1", RunID: "run-1", PipelineID: "pl-1", EnvironmentID: "staging",
		Status: model.PromotionAwaitingApproval, Strategy: "manual", RequiredApprovers: 2,
	}
	if err := st.Promotions.CreatePromotion(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := st.Promotions.GetPromotion(ctx, "run-1", "staging")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "promo-1" || got.RequiredApprovers != 2 || got.Status != model.PromotionAwaitingApproval {
		t.Errorf("get mismatch: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be stamped on create")
	}

	list, err := st.Promotions.ListPromotions(ctx, "run-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "promo-1" {
		t.Fatalf("list mismatch: %+v", list)
	}
}

func TestPromotionStore_CreateDuplicateConflict(t *testing.T) {
	st := New()
	ctx := context.Background()
	p := &model.RunPromotion{ID: "a", RunID: "r", EnvironmentID: "e", Status: model.PromotionPending}
	if err := st.Promotions.CreatePromotion(ctx, p); err != nil {
		t.Fatal(err)
	}
	dup := &model.RunPromotion{ID: "b", RunID: "r", EnvironmentID: "e", Status: model.PromotionPending}
	if err := st.Promotions.CreatePromotion(ctx, dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate (run,env), got %v", err)
	}
}

func TestPromotionStore_GetNotFound(t *testing.T) {
	st := New()
	if _, err := st.Promotions.GetPromotion(context.Background(), "ghost", "env"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPromotionStore_AddApprovalDistinctAndIdempotent(t *testing.T) {
	st := New()
	ctx := context.Background()
	p := &model.RunPromotion{ID: "p1", RunID: "r1", EnvironmentID: "prod", Status: model.PromotionAwaitingApproval, RequiredApprovers: 2}
	if err := st.Promotions.CreatePromotion(ctx, p); err != nil {
		t.Fatal(err)
	}

	added, count, err := st.Promotions.AddApproval(ctx, &model.PromotionApproval{ID: "ap1", PromotionID: "p1", ApproverSub: "alice"})
	if err != nil || !added || count != 1 {
		t.Fatalf("first approval: added=%v count=%d err=%v", added, count, err)
	}

	// Same approver again: idempotent no-op, count unchanged.
	added, count, err = st.Promotions.AddApproval(ctx, &model.PromotionApproval{ID: "ap2", PromotionID: "p1", ApproverSub: "alice"})
	if err != nil || added || count != 1 {
		t.Fatalf("duplicate approver should not raise count: added=%v count=%d err=%v", added, count, err)
	}

	// Distinct approver: count goes to 2.
	added, count, err = st.Promotions.AddApproval(ctx, &model.PromotionApproval{ID: "ap3", PromotionID: "p1", ApproverSub: "bob"})
	if err != nil || !added || count != 2 {
		t.Fatalf("second distinct approver: added=%v count=%d err=%v", added, count, err)
	}

	got, err := st.Promotions.GetPromotion(ctx, "r1", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if got.ApprovalCount() != 2 {
		t.Errorf("hydrated approvals = %d, want 2", got.ApprovalCount())
	}
}

func TestPromotionStore_AddApprovalUnknownPromotion(t *testing.T) {
	st := New()
	if _, _, err := st.Promotions.AddApproval(context.Background(), &model.PromotionApproval{ID: "x", PromotionID: "missing", ApproverSub: "a"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown promotion, got %v", err)
	}
}

func TestPromotionStore_UpdateStatus(t *testing.T) {
	st := New()
	ctx := context.Background()
	p := &model.RunPromotion{ID: "p1", RunID: "r1", EnvironmentID: "e", Status: model.PromotionAwaitingApproval}
	if err := st.Promotions.CreatePromotion(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.Promotions.UpdatePromotionStatus(ctx, "p1", model.PromotionApproved, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := st.Promotions.GetPromotion(ctx, "r1", "e")
	if got.Status != model.PromotionApproved {
		t.Errorf("status = %s, want approved", got.Status)
	}
	if err := st.Promotions.UpdatePromotionStatus(ctx, "ghost", model.PromotionDeployed, nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("update missing: expected ErrNotFound, got %v", err)
	}
}

// TestPromotionStore_GetReturnsCopy guards against the hydrate path
// handing out the stored pointer (mutating the result must not corrupt
// the store).
func TestPromotionStore_GetReturnsCopy(t *testing.T) {
	st := New()
	ctx := context.Background()
	_ = st.Promotions.CreatePromotion(ctx, &model.RunPromotion{ID: "p", RunID: "r", EnvironmentID: "e", Status: model.PromotionPending})
	_, _, _ = st.Promotions.AddApproval(ctx, &model.PromotionApproval{ID: "a", PromotionID: "p", ApproverSub: "x"})

	got, _ := st.Promotions.GetPromotion(ctx, "r", "e")
	got.Status = model.PromotionFailed
	got.Approvals = append(got.Approvals, model.PromotionApproval{ID: "evil"})

	again, _ := st.Promotions.GetPromotion(ctx, "r", "e")
	if again.Status == model.PromotionFailed {
		t.Error("mutating a returned promotion corrupted the store status")
	}
	if again.ApprovalCount() != 1 {
		t.Errorf("mutating returned approvals corrupted the store: count=%d", again.ApprovalCount())
	}
}
