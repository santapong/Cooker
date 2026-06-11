package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestStageApprovalStore_CreateGetListParity(t *testing.T) {
	st := New()
	ctx := context.Background()

	g := &model.StageApproval{
		ID: "gate-1", RunID: "run-1", StageID: "stage-1",
		Status: model.StageApprovalAwaiting, RequiredApprovers: 2,
	}
	if err := st.StageApprovals.CreateGate(ctx, g); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := st.StageApprovals.GetGate(ctx, "run-1", "stage-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "gate-1" || got.RequiredApprovers != 2 || got.Status != model.StageApprovalAwaiting {
		t.Errorf("get mismatch: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be stamped on create")
	}

	list, err := st.StageApprovals.ListGates(ctx, "run-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "gate-1" {
		t.Fatalf("list mismatch: %+v", list)
	}
}

func TestStageApprovalStore_CreateDuplicateConflict(t *testing.T) {
	st := New()
	ctx := context.Background()
	g := &model.StageApproval{ID: "a", RunID: "r", StageID: "s", Status: model.StageApprovalAwaiting}
	if err := st.StageApprovals.CreateGate(ctx, g); err != nil {
		t.Fatal(err)
	}
	dup := &model.StageApproval{ID: "b", RunID: "r", StageID: "s", Status: model.StageApprovalAwaiting}
	if err := st.StageApprovals.CreateGate(ctx, dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate (run,stage), got %v", err)
	}
}

func TestStageApprovalStore_GetNotFound(t *testing.T) {
	st := New()
	if _, err := st.StageApprovals.GetGate(context.Background(), "ghost", "stage"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStageApprovalStore_AddVoteDistinctAndIdempotent(t *testing.T) {
	st := New()
	ctx := context.Background()
	g := &model.StageApproval{ID: "gate-1", RunID: "r", StageID: "s", Status: model.StageApprovalAwaiting, RequiredApprovers: 2}
	if err := st.StageApprovals.CreateGate(ctx, g); err != nil {
		t.Fatal(err)
	}

	added, count, err := st.StageApprovals.AddVote(ctx, &model.StageApprovalVote{
		ID: "v1", StageApprovalID: "gate-1", ApproverSub: "sub-a", ApproverEmail: "a@x.com",
	})
	if err != nil || !added || count != 1 {
		t.Fatalf("first vote: added=%v count=%d err=%v, want true/1/nil", added, count, err)
	}

	// Same approver again: no new row, count unchanged.
	added, count, err = st.StageApprovals.AddVote(ctx, &model.StageApprovalVote{
		ID: "v1b", StageApprovalID: "gate-1", ApproverSub: "sub-a", ApproverEmail: "a@x.com",
	})
	if err != nil || added || count != 1 {
		t.Fatalf("duplicate vote: added=%v count=%d err=%v, want false/1/nil", added, count, err)
	}

	// Distinct approver: count advances.
	added, count, err = st.StageApprovals.AddVote(ctx, &model.StageApprovalVote{
		ID: "v2", StageApprovalID: "gate-1", ApproverSub: "sub-b", ApproverEmail: "b@x.com",
	})
	if err != nil || !added || count != 2 {
		t.Fatalf("second distinct vote: added=%v count=%d err=%v, want true/2/nil", added, count, err)
	}

	// Votes hydrate on read.
	got, err := st.StageApprovals.GetGate(ctx, "r", "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Votes) != 2 {
		t.Errorf("hydrated votes = %d, want 2", len(got.Votes))
	}
}

func TestStageApprovalStore_AddVoteUnknownGate(t *testing.T) {
	st := New()
	_, _, err := st.StageApprovals.AddVote(context.Background(), &model.StageApprovalVote{
		ID: "v1", StageApprovalID: "ghost", ApproverSub: "sub-a",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing gate, got %v", err)
	}
}

func TestStageApprovalStore_UpdateStatus(t *testing.T) {
	st := New()
	ctx := context.Background()
	g := &model.StageApproval{ID: "gate-1", RunID: "r", StageID: "s", Status: model.StageApprovalAwaiting}
	if err := st.StageApprovals.CreateGate(ctx, g); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := st.StageApprovals.UpdateGateStatus(ctx, "gate-1", model.StageApprovalApproved, "a@x.com", &now); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := st.StageApprovals.GetGate(ctx, "r", "s")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StageApprovalApproved {
		t.Errorf("status = %s, want approved", got.Status)
	}
	if got.ResolvedBy != "a@x.com" {
		t.Errorf("resolvedBy = %q, want a@x.com", got.ResolvedBy)
	}
	if got.ResolvedAt == nil {
		t.Error("ResolvedAt should be stamped")
	}

	if err := st.StageApprovals.UpdateGateStatus(ctx, "ghost", model.StageApprovalApproved, "", nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update missing gate: expected ErrNotFound, got %v", err)
	}
}

// TestStageApprovalStore_GetReturnsCopy verifies the memory store hands out
// a defensive copy: mutating a returned gate (or its votes) must not corrupt
// the stored row, matching the Postgres impl which returns fresh structs.
func TestStageApprovalStore_GetReturnsCopy(t *testing.T) {
	st := New()
	ctx := context.Background()
	g := &model.StageApproval{ID: "gate-1", RunID: "r", StageID: "s", Status: model.StageApprovalAwaiting}
	if err := st.StageApprovals.CreateGate(ctx, g); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.StageApprovals.AddVote(ctx, &model.StageApprovalVote{ID: "v1", StageApprovalID: "gate-1", ApproverSub: "sub-a"}); err != nil {
		t.Fatal(err)
	}

	got, _ := st.StageApprovals.GetGate(ctx, "r", "s")
	got.Status = model.StageApprovalRejected
	if len(got.Votes) > 0 {
		got.Votes[0].ApproverSub = "mutated"
	}

	again, _ := st.StageApprovals.GetGate(ctx, "r", "s")
	if again.Status != model.StageApprovalAwaiting {
		t.Errorf("stored status mutated to %s", again.Status)
	}
	if len(again.Votes) > 0 && again.Votes[0].ApproverSub != "sub-a" {
		t.Errorf("stored vote mutated to %s", again.Votes[0].ApproverSub)
	}
}
