package vercel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"

	storepg "github.com/santapong/cooker/internal/store/postgres"
)

func TestProjectCoverageKeepsSelectionAndLastObservationSeparate(t *testing.T) {
	ctx := context.Background()
	svc, api, c := newFixture(t, NewMemory())
	if c.Coverage != nil {
		t.Fatal("new connection invented discovery coverage")
	}
	c = selectProjects(t, svc, c, "prj_one")
	c, err := svc.Sync(ctx, c.ID, true)
	if err != nil || c.Coverage == nil || c.Coverage.AccessibleProjects != 2 || c.Coverage.SelectedProjects != 1 || c.Coverage.ObservedAt.IsZero() {
		t.Fatalf("missing complete scoped coverage: %v", err)
	}
	observed := c.Coverage.ObservedAt
	view, err := svc.View(ctx)
	if err != nil || len(view.Projects) != 1 || view.Projects[0].ID != "prj_one" {
		t.Fatal("discovery automatically imported an unselected project")
	}
	view.Connections[0].Coverage.AccessibleProjects = 500
	r, err := svc.Store.Get(ctx, c.ID)
	if err != nil || r.Snapshot.Coverage.AccessibleProjects != 2 {
		t.Fatal("public coverage mutated persisted inventory")
	}
	api.failure = errors.New("fixture-core-failure")
	failed, err := svc.Sync(ctx, c.ID, true)
	if err != nil || failed.SyncError == "" || failed.Coverage == nil || !failed.Coverage.ObservedAt.Equal(observed) {
		t.Fatal("failure replaced or refreshed the last successful discovery")
	}
	c = selectProjects(t, svc, failed, "prj_two")
	if c.Coverage != nil {
		t.Fatal("changed selection claimed old counts as current")
	}
	var legacy Snapshot
	if err := json.Unmarshal([]byte(`{"projects":[],"deployments":[]}`), &legacy); err != nil || legacy.Coverage != nil {
		t.Fatal("old snapshot did not remain readable with unknown coverage")
	}
}

func TestProjectCoveragePostgresRestartAndOldSnapshot(t *testing.T) {
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL required")
	}
	ctx := context.Background()
	st, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc, _, c := newFixture(t, &Postgres{DB: db})
	defer db.ExecContext(ctx, `DELETE FROM vercel_connections WHERE id=$1`, c.ID)
	c = selectProjects(t, svc, c, "prj_one")
	c, err = svc.Sync(ctx, c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Service{Store: &Postgres{DB: db}, Codec: svc.Codec, API: svc.API}
	view, err := restarted.View(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got *Connection
	for _, connection := range view.Connections {
		if connection.ID == c.ID {
			got = connection
		}
	}
	if got == nil || got.Coverage == nil || got.Coverage.AccessibleProjects != 2 || !got.Coverage.ObservedAt.Equal(c.Coverage.ObservedAt) {
		t.Fatal("discovery coverage did not survive restart")
	}
	if _, err := db.ExecContext(ctx, `UPDATE vercel_connections SET snapshot=snapshot-'coverage' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	r, err := restarted.Store.Get(ctx, c.ID)
	if err != nil || publicConnection(r, c.Coverage.ObservedAt).Coverage != nil || len(r.Snapshot.Projects) != 1 {
		t.Fatal("legacy PostgreSQL snapshot lost inventory or invented coverage")
	}
}
