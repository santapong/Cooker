package postgres

// Live-Postgres integration tests for the PromotionStore's AddApproval
// transaction (the C-promotion tail out of scope for the service/memory
// tests, which only exercise the distinct-approver *behaviour*, not the
// Postgres transaction *mechanics*).
//
// AddApproval runs two statements — an INSERT (approval row, guarded by
// UNIQUE(promotion_id, approver_sub)) and a COUNT/advance read — inside a
// single transaction (DA-H2). These tests assert that a failure landing
// between those two statements rolls back cleanly (no orphaned approval
// row, no phantom count), and that the UNIQUE constraint makes a
// concurrent double-approval by the same approver collapse to exactly one
// row with no double count-advance.
//
// Skip-if-no-DB: same convention as license_test.go — needs
// COOKER_INTEGRATION_DATABASE_URL set. The default `go test ./...` run
// compiles this file but every test skips immediately when the env var is
// empty, so it's a no-op without a database and runs for real in a
// DB-backed CI job:
//
//	COOKER_INTEGRATION_DATABASE_URL=postgres://...?sslmode=disable \
//	  go test -tags=integration ./internal/store/postgres/... -count=1 -race

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/santapong/cooker/internal/model"
)

// promotionsSchema is the inline copy of migration 020_run_promotions.up.sql
// (the run_promotions + promotion_approvals tables only — this suite owns
// both and recreates them from a clean slate for each test).
const promotionsSchema = `
CREATE TABLE IF NOT EXISTS run_promotions (
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL,
    pipeline_id        TEXT NOT NULL DEFAULT '',
    environment_id     TEXT NOT NULL,
    status             TEXT NOT NULL,
    strategy           TEXT NOT NULL DEFAULT 'manual',
    required_approvers INT  NOT NULL DEFAULT 0,
    requested_by       TEXT NOT NULL DEFAULT '',
    promoted_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, environment_id)
);

CREATE INDEX IF NOT EXISTS idx_run_promotions_run
    ON run_promotions (run_id);

CREATE TABLE IF NOT EXISTS promotion_approvals (
    id             TEXT PRIMARY KEY,
    promotion_id   TEXT NOT NULL REFERENCES run_promotions(id) ON DELETE CASCADE,
    approver_sub   TEXT NOT NULL DEFAULT '',
    approver_email TEXT NOT NULL DEFAULT '',
    note           TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (promotion_id, approver_sub)
);

CREATE INDEX IF NOT EXISTS idx_promotion_approvals_promotion
    ON promotion_approvals (promotion_id);
`

// dialPromotionsDB opens the integration database or skips. It drops and
// recreates the promotion tables so each test starts clean — this suite
// owns both tables.
func dialPromotionsDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL not set; skipping live-DB promotion store tests")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS promotion_approvals, run_promotions CASCADE`); err != nil {
		t.Fatalf("drop promotion tables: %v", err)
	}
	if _, err := db.Exec(promotionsSchema); err != nil {
		t.Fatalf("create promotion tables: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// mkLivePromotion seeds a single awaiting-approval promotion so AddApproval
// has a live promotion_id (and satisfying FK) to attach approvals to.
func mkLivePromotion(t *testing.T, ctx context.Context, st *PromotionStore, id string) {
	t.Helper()
	p := &model.RunPromotion{
		ID: id, RunID: "run-" + id, EnvironmentID: "prod",
		Status: model.PromotionAwaitingApproval, RequiredApprovers: 2,
	}
	if err := st.CreatePromotion(ctx, p); err != nil {
		t.Fatalf("seed promotion %s: %v", id, err)
	}
}

// TestLive_AddApproval_MidTxCancelRollsBackCleanly drives a failure
// between AddApproval's two statements (the approval INSERT and the
// COUNT/advance read) via the afterApprovalInsertHook seam, which cancels
// the transaction's context right after the INSERT commits within the tx
// but before the COUNT runs. It asserts the whole transaction rolls back:
// AddApproval returns an error, no approval row is left behind (no
// orphaned INSERT), and a subsequent successful approval still starts the
// count from zero (no phantom/partial advance survived the failed
// attempt).
func TestLive_AddApproval_MidTxCancelRollsBackCleanly(t *testing.T) {
	db := dialPromotionsDB(t)
	st := NewPromotionStore(db)
	ctx := context.Background()
	mkLivePromotion(t, ctx, st, "p1")

	// Install the mid-tx failure seam for this test only, then restore the
	// production no-op so other tests in the package are unaffected.
	prevHook := afterApprovalInsertHook
	t.Cleanup(func() { afterApprovalInsertHook = prevHook })

	cancelCtx, cancel := context.WithCancel(ctx)
	afterApprovalInsertHook = func() { cancel() }

	added, count, err := st.AddApproval(cancelCtx, &model.PromotionApproval{
		ID: "ap1", PromotionID: "p1", ApproverSub: "alice",
	})
	if err == nil {
		t.Fatalf("expected AddApproval to fail when ctx is cancelled mid-transaction; got added=%v count=%d", added, count)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected the mid-tx failure to surface as context.Canceled; got %v", err)
	}

	// No orphaned approval row: the INSERT must have rolled back with the
	// rest of the transaction, even though it committed to the driver
	// stream before the cancellation landed.
	var rows int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM promotion_approvals WHERE promotion_id = $1`, "p1").Scan(&rows); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if rows != 0 {
		t.Fatalf("expected no orphaned approval row after mid-tx failure; found %d", rows)
	}

	// No phantom count/state advance: GetPromotion must still show zero
	// approvals and the pre-gate status.
	got, err := st.GetPromotion(ctx, "run-p1", "prod")
	if err != nil {
		t.Fatalf("GetPromotion after rollback: %v", err)
	}
	if got.ApprovalCount() != 0 {
		t.Fatalf("expected 0 approvals after rollback; got %d", got.ApprovalCount())
	}
	if got.Status != model.PromotionAwaitingApproval {
		t.Fatalf("rolled-back AddApproval must not advance status; got %s", got.Status)
	}

	// Restore the no-op hook and confirm a real approval now succeeds
	// cleanly and starts counting from zero — the failed attempt left no
	// residue for the next call to trip over.
	afterApprovalInsertHook = prevHook
	added, count, err = st.AddApproval(ctx, &model.PromotionApproval{
		ID: "ap2", PromotionID: "p1", ApproverSub: "alice",
	})
	if err != nil {
		t.Fatalf("retry after rollback should succeed: %v", err)
	}
	if !added || count != 1 {
		t.Fatalf("retry after rollback: added=%v count=%d, want added=true count=1 (no phantom bump from the failed attempt)", added, count)
	}
}

// TestLive_AddApproval_MidTxDeadlineExceededRollsBack complements the
// explicit-cancel test above with a second, independent trigger for the
// same mid-tx failure class: instead of an external cancel() call landing
// between the two statements, the context's own deadline expires there.
// This proves the rollback property does not depend on *how* ctx.Err()
// becomes non-nil (an explicit CancelFunc vs. a deadline firing), only on
// where — after the INSERT, before the COUNT/advance.
func TestLive_AddApproval_MidTxDeadlineExceededRollsBack(t *testing.T) {
	db := dialPromotionsDB(t)
	st := NewPromotionStore(db)
	ctx := context.Background()
	mkLivePromotion(t, ctx, st, "p2")

	prevHook := afterApprovalInsertHook
	t.Cleanup(func() { afterApprovalInsertHook = prevHook })

	deadlineCtx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer cancel()
	afterApprovalInsertHook = func() {
		// Block past the deadline so ctx.Err() is DeadlineExceeded by
		// the time the COUNT statement runs, without racing a
		// goroutine against the driver.
		<-deadlineCtx.Done()
	}

	added, count, err := st.AddApproval(deadlineCtx, &model.PromotionApproval{
		ID: "ap1", PromotionID: "p2", ApproverSub: "carol",
	})
	if err == nil {
		t.Fatalf("expected AddApproval to fail once the deadline expires mid-transaction; got added=%v count=%d", added, count)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected the mid-tx failure to surface as context.DeadlineExceeded; got %v", err)
	}

	var rows int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM promotion_approvals WHERE promotion_id = $1`, "p2").Scan(&rows); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if rows != 0 {
		t.Fatalf("expected no orphaned approval row after mid-tx failure; found %d", rows)
	}

	got, err := st.GetPromotion(ctx, "run-p2", "prod")
	if err != nil {
		t.Fatalf("GetPromotion after rollback: %v", err)
	}
	if got.ApprovalCount() != 0 {
		t.Fatalf("expected 0 approvals after rollback; got %d", got.ApprovalCount())
	}
}

// TestLive_AddApproval_ConcurrentSameApprover_ExactlyOneRowNoDoubleAdvance
// fires two concurrent AddApproval calls for the same (promotion,
// approver) pair — the classic "double-click" race. The
// UNIQUE(promotion_id, approver_sub) constraint plus ON CONFLICT DO
// NOTHING must collapse this to exactly one persisted row and exactly one
// "added=true" result; the other call observes added=false. The gate must
// not advance twice: the final count is 1, not 2.
func TestLive_AddApproval_ConcurrentSameApprover_ExactlyOneRowNoDoubleAdvance(t *testing.T) {
	db := dialPromotionsDB(t)
	st := NewPromotionStore(db)
	ctx := context.Background()
	mkLivePromotion(t, ctx, st, "p3")

	const n = 8
	var wg sync.WaitGroup
	addedCount := make([]bool, n)
	counts := make([]int, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			added, count, err := st.AddApproval(ctx, &model.PromotionApproval{
				ID: idFor(i), PromotionID: "p3", ApproverSub: "dave",
			})
			addedCount[i], counts[i], errs[i] = added, count, err
		}(i)
	}
	wg.Wait()

	var trueAdds int
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent AddApproval[%d] unexpected error: %v", i, errs[i])
		}
		if addedCount[i] {
			trueAdds++
		}
	}
	if trueAdds != 1 {
		t.Fatalf("expected exactly one concurrent caller to win the insert (added=true); got %d of %d", trueAdds, n)
	}

	var rows int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM promotion_approvals WHERE promotion_id = $1 AND approver_sub = 'dave'`,
		"p3").Scan(&rows); err != nil {
		t.Fatalf("count rows for dave: %v", err)
	}
	if rows != 1 {
		t.Fatalf("UNIQUE(promotion_id, approver_sub) should collapse concurrent double-approval to exactly one row; found %d", rows)
	}

	got, err := st.GetPromotion(ctx, "run-p3", "prod")
	if err != nil {
		t.Fatalf("GetPromotion: %v", err)
	}
	if got.ApprovalCount() != 1 {
		t.Fatalf("gate must not double-advance on a same-approver race; approval count = %d, want 1", got.ApprovalCount())
	}
	// Every winning/losing caller must have observed the same final,
	// post-conflict-resolution count — no caller saw a transient over-count.
	for i := 0; i < n; i++ {
		if counts[i] != 1 {
			t.Errorf("caller %d observed count=%d, want 1 (post-conflict-resolution count must be stable)", i, counts[i])
		}
	}
}

func idFor(i int) string {
	return "ap-" + string(rune('a'+i))
}
