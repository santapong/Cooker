package vercel

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

func TestPostgresEncryptedConnectionSnapshotAndFence(t *testing.T) {
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
	svc, api, c := newFixture(t, &Postgres{DB: db})
	defer func() { _, _ = db.ExecContext(ctx, `DELETE FROM vercel_connections WHERE id=$1`, c.ID) }()
	c = selectProjects(t, svc, c, "prj_one")
	if _, err = svc.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	// A new service instance reads durable provider provenance, not process memory.
	restarted := &Service{Store: &Postgres{DB: db}, Codec: svc.Codec, API: svc.API}
	view, err := restarted.View(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range view.Deployments {
		if d.ConnectionID == c.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("snapshot not durable")
	}
	var data []byte
	var config string
	if err = db.QueryRowContext(ctx, `SELECT credential,configuration::text FROM vercel_connections WHERE id=$1`, c.ID).Scan(&data, &config); err != nil {
		t.Fatal(err)
	}
	if string(data) == testToken {
		t.Fatal("plaintext persisted")
	}
	plaintext, err := svc.Codec.Open(data)
	if err != nil || string(plaintext) != testToken {
		t.Fatal("wrong encryption")
	}
	now := time.Now().UTC()
	lease, err := svc.Store.Claim(ctx, c.ID, c.Version, now, now.Add(time.Minute), true)
	if err != nil {
		t.Fatal(err)
	}
	c = selectProjects(t, svc, c, "prj_two")
	if err = svc.Store.Finish(ctx, c.ID, c.Version-1, lease, &Snapshot{Projects: []Project{}, Deployments: []Deployment{}}, "", now, nil); err == nil {
		t.Fatal("database allowed stale sync after config change")
	}
	// Synchronize the changed scope, then test durable failure retention/backoff.
	if _, err = svc.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Store.Get(ctx, c.ID)
	if err != nil || r.LastSuccessAt == nil || len(r.Snapshot.Deployments) != 1 {
		t.Fatal("scope update did not synchronize")
	}
	api.failure = &APIError{StatusCode: 429, Message: "Vercel rate limit reached", RetryAfter: 5 * time.Minute}
	failed, err := svc.Sync(ctx, c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if failed.RetryAt == nil || failed.SyncError == "" || !failed.LastSuccessAt.Equal(*r.LastSuccessAt) {
		t.Fatal("database lost last-good status or provider backoff")
	}
	if _, err = svc.Sync(ctx, c.ID, true); !errors.Is(err, ErrBusy) {
		t.Fatal("database allowed backoff bypass")
	}
	retained, err := svc.Store.Get(ctx, c.ID)
	if err != nil || len(retained.Snapshot.Deployments) != 1 || retained.Snapshot.Deployments[0].ProjectID != "prj_two" {
		t.Fatal("database replaced the last-good snapshot on failure")
	}
	if err = svc.Delete(ctx, c.ID, c.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Store.Get(ctx, c.ID); err == nil {
		t.Fatal("disconnect retained encrypted credentials")
	}
}
