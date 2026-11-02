package vercel

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	storepg "github.com/santapong/cooker/internal/store/postgres"
)

func TestPostgresDetailsRoundTripAndLegacySnapshot(t *testing.T) {
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL required")
	}
	ctx := context.Background()
	st, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	svc, api, c := detailService(t, &Postgres{DB: db}, 1)
	defer func() { _, _ = db.ExecContext(ctx, `DELETE FROM vercel_connections WHERE id=$1`, c.ID) }()
	// Existing migration032 JSONB records have no details fields; remain readable.
	legacy := `{"projects":[{"id":"prj_00","name":"Legacy","framework":"vite"}],"deployments":[{"id":"dpl_legacy","projectId":"prj_00","name":"Legacy","state":"READY","target":"production","createdAt":"2026-01-01T00:00:00Z"}]}`
	if _, err = db.ExecContext(ctx, `UPDATE vercel_connections SET snapshot=$2::jsonb WHERE id=$1`, c.ID, legacy); err != nil {
		t.Fatal(err)
	}
	old, err := svc.Store.Get(ctx, c.ID)
	if err != nil || old.Snapshot.Projects[0].Details != nil || old.Snapshot.Deployments[0].Details != nil {
		t.Fatal("legacy snapshot compatibility lost")
	}
	if _, err = svc.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	restarted := &Service{Store: &Postgres{DB: db}, Codec: svc.Codec, API: svc.API}
	current, err := restarted.Store.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	details := current.Snapshot.Projects[0].Details
	if details == nil || details.ProductionDeployment == nil || details.ProductionDeployment.ID != "dpl_current_prj_00" || !details.ProductionDeployment.Details.ServicesKnown || len(current.Snapshot.Deployments) != 2 {
		t.Fatal("enriched snapshot not durable")
	}
	observed := details.ObservedAt
	api.detailError = &APIError{StatusCode: 429, RetryAfter: 5 * time.Minute, Message: "discarded raw message"}
	receipt, err := restarted.Sync(ctx, c.ID, true)
	if err != nil || receipt.RetryAt == nil {
		t.Fatalf("optional cooldown not durable: %v", err)
	}
	current, err = restarted.Store.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Snapshot.Projects[0].Details.ObservedAt.Equal(observed) || current.Snapshot.Projects[0].Details.Error == "" || current.LastSuccessAt == nil || len(current.Snapshot.Deployments) != 2 {
		t.Fatal("partial optional failure erased retained data")
	}
	if _, err = restarted.Sync(ctx, c.ID, true); !errors.Is(err, ErrBusy) {
		t.Fatal("database optional cooldown bypassed")
	}
}
