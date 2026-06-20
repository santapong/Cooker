package postgres

// Live-Postgres integration tests for the LicenseStore (M2-03). These
// exercise the real SQL — the single-row upsert, the TEXT[] features
// round-trip, NULL vs non-NULL expires_at, Delete, and the
// licenses_one_row CHECK — against an actual database.
//
// Skip-if-no-DB: the suite needs COOKER_INTEGRATION_DATABASE_URL set
// (same convention as internal/jobqueue and internal/scheduler). The
// default `go test ./internal/store/... -race` gate compiles this file
// but skips every test when the env var is empty, so it is a no-op for
// developers without a database and runs for real in a DB-backed CI job:
//
//	COOKER_INTEGRATION_DATABASE_URL=postgres://...?sslmode=disable \
//	  go test ./internal/store/postgres/... -count=1 -race

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"database/sql"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// licensesSchema is the inline copy of migration 024_licenses.up.sql,
// kept here so the test owns and recreates its table from a clean slate.
// The licenses_one_row CHECK is the load-bearing assertion in
// TestLive_License_OneRowCheckRejectsNonActive below.
const licensesSchema = `
CREATE TABLE IF NOT EXISTS licenses (
    id                 TEXT PRIMARY KEY,
    raw_token          TEXT NOT NULL,
    plan               TEXT NOT NULL,
    seats              INTEGER NOT NULL DEFAULT 0,
    features           TEXT[] NOT NULL DEFAULT '{}',
    customer           TEXT NOT NULL DEFAULT '',
    issued_at          TIMESTAMPTZ NOT NULL,
    expires_at         TIMESTAMPTZ,
    installed_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    installed_by_sub   TEXT NOT NULL DEFAULT '',
    installed_by_email TEXT NOT NULL DEFAULT '',
    CONSTRAINT licenses_one_row CHECK (id = 'active')
);
`

// dialLicensesDB opens the integration database or skips. It drops and
// recreates the licenses table so each test starts clean — this suite
// owns the table.
func dialLicensesDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL not set; skipping live-DB license store tests")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS licenses CASCADE`); err != nil {
		t.Fatalf("drop licenses: %v", err)
	}
	if _, err := db.Exec(licensesSchema); err != nil {
		t.Fatalf("create licenses: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mkLiveLicense(plan string, expires *time.Time) *model.License {
	return &model.License{
		// A non-sentinel id on purpose: Set must normalise it to "active",
		// matching the memory impl (parity guarantee, M2-02).
		ID:               "11111111-2222-3333-4444-555555555555",
		RawToken:         "ck_lic_" + plan,
		Plan:             plan,
		Seats:            3,
		Features:         []string{"multi-replica", "oidc"},
		Customer:         "Acme Corp",
		IssuedAt:         time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:        expires,
		InstalledBySub:   "alice",
		InstalledByEmail: "alice@x.com",
	}
}

// TestLive_License_RoundTrip covers Set→GetActive for both a non-null
// expiry and a perpetual (NULL expires_at) license, asserts the TEXT[]
// features survive the round-trip, that Set normalises the id to the
// "active" sentinel (M2-02 parity with memory), and that a second Set
// replaces in place (still a single row).
func TestLive_License_RoundTrip(t *testing.T) {
	db := dialLicensesDB(t)
	st := NewLicenseStore(db)
	ctx := context.Background()

	// Nothing installed yet.
	if _, err := st.GetActive(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetActive with no license should be ErrNotFound; got %v", err)
	}

	// Non-null expiry.
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	lic := mkLiveLicense("crew", &exp)
	if err := st.Set(ctx, lic); err != nil {
		t.Fatalf("set: %v", err)
	}
	if lic.ID != activeLicenseID {
		t.Fatalf("Set should normalise caller's id to %q; got %q", activeLicenseID, lic.ID)
	}
	if lic.InstalledAt.IsZero() {
		t.Fatalf("Set should stamp InstalledAt when zero")
	}

	got, err := st.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if got.ID != activeLicenseID {
		t.Fatalf("GetActive should return id %q; got %q", activeLicenseID, got.ID)
	}
	if got.Plan != "crew" || got.Seats != 3 || got.Customer != "Acme Corp" {
		t.Fatalf("unexpected license: %+v", got)
	}
	if len(got.Features) != 2 || got.Features[0] != "multi-replica" || got.Features[1] != "oidc" {
		t.Fatalf("features TEXT[] round-trip failed: %+v", got.Features)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("expiresAt round-trip failed: %v", got.ExpiresAt)
	}
	if got.InstalledBySub != "alice" || got.InstalledByEmail != "alice@x.com" {
		t.Fatalf("installer fields round-trip failed: %+v", got)
	}

	// Set again replaces in place: perpetual license (NULL expires_at).
	if err := st.Set(ctx, mkLiveLicense("constellation", nil)); err != nil {
		t.Fatalf("replace set: %v", err)
	}
	got, err = st.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive after replace: %v", err)
	}
	if got.Plan != "constellation" {
		t.Fatalf("replace did not take: %+v", got)
	}
	if got.ExpiresAt != nil {
		t.Fatalf("perpetual license should round-trip a NULL expires_at; got %v", got.ExpiresAt)
	}

	// Still exactly one row.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM licenses`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one license row after replace; got %d", n)
	}
}

// TestLive_License_DeleteThenGetActive confirms Delete clears the row,
// GetActive then returns ErrNotFound, and Delete is idempotent.
func TestLive_License_DeleteThenGetActive(t *testing.T) {
	db := dialLicensesDB(t)
	st := NewLicenseStore(db)
	ctx := context.Background()

	exp := time.Now().Add(time.Hour)
	if err := st.Set(ctx, mkLiveLicense("crew", &exp)); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := st.Delete(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetActive(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetActive after delete should be ErrNotFound; got %v", err)
	}
	// Idempotent: deleting when nothing is installed is a no-op.
	if err := st.Delete(ctx); err != nil {
		t.Fatalf("idempotent delete should be a no-op; got %v", err)
	}
}

// TestLive_License_OneRowCheckRejectsNonActive asserts the
// licenses_one_row CHECK rejects a non-'active' id. The store API never
// writes a non-sentinel id (Set forces 'active'), so this drives the raw
// SQL to prove the schema guard is the backstop behind that invariant.
func TestLive_License_OneRowCheckRejectsNonActive(t *testing.T) {
	db := dialLicensesDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx,
		`INSERT INTO licenses (id, raw_token, plan, issued_at)
		 VALUES ($1, 'ck_lic_x', 'crew', NOW())`, "not-active")
	if err == nil {
		t.Fatal("expected licenses_one_row CHECK to reject a non-'active' id")
	}

	// And the store API always yields id 'active' regardless of input.
	st := NewLicenseStore(db)
	lic := mkLiveLicense("crew", nil)
	if err := st.Set(ctx, lic); err != nil {
		t.Fatalf("set: %v", err)
	}
	if lic.ID != activeLicenseID {
		t.Fatalf("store must yield id %q; got %q", activeLicenseID, lic.ID)
	}
	got, err := st.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if got.ID != activeLicenseID {
		t.Fatalf("GetActive must yield id %q; got %q", activeLicenseID, got.ID)
	}
}
