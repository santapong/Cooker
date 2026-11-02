//go:build integration

package logstore

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func isolatedLogDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL is required")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("cooker_log_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	migration, err := os.ReadFile("../store/postgres/migrations/029_stage_logs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPostgresLogPersistenceCursorsAndBounds(t *testing.T) {
	db := isolatedLogDB(t)
	ctx := context.Background()
	p := NewPostgres(db, Limits{MaxBytesPerStage: 1024, MaxStreams: 2, Retention: time.Hour})
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.AppendNext(ctx, "r", "s", Entry{Line: "line", TS: time.Now()})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// A newly constructed store represents another worker after restart.
	reopened := NewPostgres(db, p.limits)
	entries, latest, err := reopened.History(ctx, "r", "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 32 || latest != 32 {
		t.Fatalf("restart history=%d cursor=%d", len(entries), latest)
	}
	for i, e := range entries {
		if e.Seq != i+1 {
			t.Fatalf("concurrent cursor gap at %d: %d", i, e.Seq)
		}
	}
	if err := p.Append(ctx, "r", "s", Entry{Seq: 1, Line: "overwrite"}); err == nil {
		t.Fatal("stale explicit cursor accepted")
	}
	trimmed := NewPostgres(db, Limits{MaxBytesPerStage: 8, MaxStreams: 2, Retention: time.Hour})
	e, err := trimmed.AppendNext(ctx, "r", "s", Entry{Line: "newest"})
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err = trimmed.History(ctx, "r", "s", 0)
	if err != nil || len(entries) != 1 || entries[0].Seq != 33 || e.Seq != 33 {
		t.Fatalf("byte cap/cursor lost: %v %v", entries, err)
	}
	for _, stage := range []string{"s2", "s3"} {
		if _, err = trimmed.AppendNext(ctx, "r", stage, Entry{Line: "more"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = trimmed.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM stage_log_streams`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("stream cap=%d err=%v", count, err)
	}
	if _, err = db.Exec(`UPDATE stage_log_streams SET updated_at=NOW()-INTERVAL '2 hours'`); err != nil {
		t.Fatal(err)
	}
	if err = trimmed.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM stage_log_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retention did not cascade: %d %v", count, err)
	}
}
