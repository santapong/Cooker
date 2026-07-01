package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

// These tests guard the distinct-approver integrity property (UNIQUE
// (promotion_id, approver_sub) + the status-check at Approve) under
// concurrency: they assert final outcomes (approval count, gate status)
// are correct when multiple goroutines race Approve/AddApproval. The
// memory store's promotions type already serializes its critical
// sections behind a single sync.RWMutex, so -race should be clean; the
// value of these tests is guarding the *outcome* invariant against a
// future refactor (e.g. splitting that mutex into finer-grained locks)
// that could silently reintroduce a lost-update or double-advance bug.
//
// Note: the memory store has no multi-statement transaction to fail
// mid-way (each call is a single critical section), so a genuine
// mid-tx-failure-injection test belongs at the Postgres layer
// (internal/store/postgres/**), which is out of this agent's allowed
// paths (backend-api may not touch internal/store/**). Flagged as a
// cooker-backend-data follow-up rather than added here.

func TestPromotionService_Approve_ConcurrentSameApprover_NoDoubleCount(t *testing.T) {
	svc := newPromotionService(&model.Environment{
		ID: "prod", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 3},
	})
	ctx := context.Background()
	if _, err := svc.Promote(ctx, "run-1", "pl-1", "prod", "op@x.com"); err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.Approve(ctx, "run-1", "prod", "sub-alice", "alice@x.com", "")
			errs[i] = err
		}(i)
	}
	wg.Wait()

	var successes, alreadyApproved int
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAlreadyApproved):
			alreadyApproved++
		default:
			t.Fatalf("unexpected error from concurrent Approve: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 success (first approval), got %d", successes)
	}
	if alreadyApproved != n-1 {
		t.Errorf("expected %d ErrAlreadyApproved results, got %d", n-1, alreadyApproved)
	}

	final, err := svc.promotions.GetPromotion(ctx, "run-1", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if final.ApprovalCount() != 1 {
		t.Errorf("final approval count = %d, want 1 (same approver, no double count)", final.ApprovalCount())
	}
	if final.Status != model.PromotionAwaitingApproval {
		t.Errorf("final status = %s, want still awaiting (1 of 3)", final.Status)
	}
}

func TestPromotionService_Approve_ConcurrentDistinctApprovers_AdvancesOnce(t *testing.T) {
	svc := newPromotionService(&model.Environment{
		ID: "prod", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 5},
	})
	ctx := context.Background()
	if _, err := svc.Promote(ctx, "run-1", "pl-1", "prod", "op@x.com"); err != nil {
		t.Fatal(err)
	}

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sub := "sub-" + string(rune('a'+i))
			_, err := svc.Approve(ctx, "run-1", "prod", sub, sub+"@x.com", "")
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error from distinct approver: %v", i, err)
		}
	}

	final, err := svc.promotions.GetPromotion(ctx, "run-1", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if final.ApprovalCount() != 5 {
		t.Errorf("final approval count = %d, want 5", final.ApprovalCount())
	}
	if final.Status != model.PromotionApproved {
		t.Errorf("final status = %s, want approved (5 of 5 distinct approvers)", final.Status)
	}
	if final.PromotedAt == nil {
		t.Error("expected PromotedAt to be stamped once the gate advances")
	}

	// A 6th, later distinct approver on an already-terminal gate must not
	// error or re-advance.
	got, err := svc.Approve(ctx, "run-1", "prod", "sub-z", "z@x.com", "late")
	if err != nil {
		t.Fatalf("post-terminal approve should not error, got: %v", err)
	}
	if got.Status != model.PromotionApproved {
		t.Errorf("post-terminal status = %s, want it to remain approved", got.Status)
	}
}

func TestPromotionService_Approve_HighConcurrencyMixed(t *testing.T) {
	svc := newPromotionService(&model.Environment{
		ID: "prod", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 10},
	})
	ctx := context.Background()
	if _, err := svc.Promote(ctx, "run-1", "pl-1", "prod", "op@x.com"); err != nil {
		t.Fatal(err)
	}

	const identities = 10
	const perIdentity = 3
	var wg sync.WaitGroup
	type result struct{ err error }
	results := make([]result, identities*perIdentity)

	idx := 0
	for i := 0; i < identities; i++ {
		sub := "sub-" + string(rune('a'+i))
		for j := 0; j < perIdentity; j++ {
			wg.Add(1)
			ri := idx
			go func(sub string) {
				defer wg.Done()
				_, err := svc.Approve(ctx, "run-1", "prod", sub, sub+"@x.com", "")
				results[ri] = result{err: err}
			}(sub)
			idx++
		}
	}
	wg.Wait()

	var successes, alreadyApproved int
	for _, r := range results {
		switch {
		case r.err == nil:
			successes++
		case errors.Is(r.err, ErrAlreadyApproved):
			alreadyApproved++
		default:
			t.Fatalf("unexpected error: %v", r.err)
		}
	}
	if successes != identities {
		t.Errorf("expected %d first-time successes (one per distinct identity), got %d", identities, successes)
	}
	if alreadyApproved != identities*(perIdentity-1) {
		t.Errorf("expected %d ErrAlreadyApproved results, got %d", identities*(perIdentity-1), alreadyApproved)
	}

	final, err := svc.promotions.GetPromotion(ctx, "run-1", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if final.ApprovalCount() != identities {
		t.Errorf("final approval count = %d, want %d", final.ApprovalCount(), identities)
	}
	if final.Status != model.PromotionApproved {
		t.Errorf("final status = %s, want approved", final.Status)
	}
}
