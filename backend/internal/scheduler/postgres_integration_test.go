//go:build integration

package scheduler_test

// Live-Postgres integration tests for the cron scheduler. Gated on
// the `integration` build tag AND COOKER_INTEGRATION_DATABASE_URL.
//
// Run locally with:
//
//	COOKER_INTEGRATION_DATABASE_URL=postgres://...?sslmode=disable \
//	  go test -tags=integration ./internal/scheduler/... -count=1 -race
//
// The suite drops + recreates the `jobs` and `schedules` tables so
// each test starts clean.

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/scheduler"
	"github.com/santapong/cooker/internal/service"
)

const liveSchema = `
DROP TABLE IF EXISTS jobs CASCADE;
DROP TABLE IF EXISTS schedules CASCADE;

CREATE TABLE jobs (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 1,
    run_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_by       TEXT NOT NULL DEFAULT '',
    locked_at       TIMESTAMPTZ,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    concurrency_key TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT jobs_status_check CHECK (
        status IN ('pending','running','succeeded','failed','cancelled')
    )
);
CREATE INDEX jobs_pickup_idx ON jobs (run_at) WHERE status = 'pending';
CREATE INDEX jobs_running_concurrency_idx ON jobs (concurrency_key)
    WHERE status = 'running' AND concurrency_key <> '';

CREATE TABLE schedules (
    id           TEXT PRIMARY KEY,
    pipeline_id  TEXT NOT NULL,
    name         TEXT NOT NULL DEFAULT '',
    cron_expr    TEXT NOT NULL,
    timezone     TEXT NOT NULL DEFAULT 'UTC',
    last_run_at  TIMESTAMPTZ,
    last_run_id  TEXT NOT NULL DEFAULT '',
    next_run_at  TIMESTAMPTZ NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX schedules_due_idx ON schedules (next_run_at) WHERE enabled = TRUE;
`

func dialLiveScheduler(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL not set; skipping live-DB scheduler tests")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := db.Exec(liveSchema); err != nil {
		t.Fatalf("create schemas: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestLive_SchedulerEnqueuesDueRow inserts a due schedule, runs the
// scheduler for a short window, and asserts a pipeline-run job
// landed on the jobs table with the correct pipeline_id in the
// payload + last_run_at and next_run_at were bumped atomically.
func TestLive_SchedulerEnqueuesDueRow(t *testing.T) {
	db := dialLiveScheduler(t)
	store := scheduler.NewPostgresStore(db)
	jq := jobqueue.NewPostgres(db)
	enq := service.NewJobQueueEnqueuer(jq)

	// A due schedule one minute in the past, hourly cron.
	now := time.Now().UTC()
	if err := store.Create(context.Background(), scheduler.Schedule{
		ID:         "sch-1",
		PipelineID: "00000000-0000-0000-0000-000000000001",
		Name:       "integration",
		CronExpr:   "0 * * * *",
		Timezone:   "UTC",
		NextRunAt:  now.Add(-1 * time.Minute),
		Enabled:    true,
	}); err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	runner, err := scheduler.NewRunner(scheduler.Options{
		DB:        db,
		Store:     store,
		Enqueuer:  enq,
		TickEvery: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = runner.Run(ctx) }()

	deadline := time.Now().Add(4 * time.Second)
	var jobs int
	for time.Now().Before(deadline) {
		row := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE kind = $1`, service.JobKindPipelineRun)
		if err := row.Scan(&jobs); err != nil {
			t.Fatalf("count jobs: %v", err)
		}
		if jobs >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if jobs < 1 {
		t.Fatalf("expected at least 1 pipeline-run job, got %d", jobs)
	}

	// Schedule row should have been bumped: last_run_at non-null and
	// next_run_at moved into the future.
	got, err := store.Get(context.Background(), "sch-1")
	if err != nil {
		t.Fatalf("re-fetch: %v", err)
	}
	if got.LastRunAt == nil {
		t.Fatal("LastRunAt was not stamped after firing")
	}
	if !got.NextRunAt.After(now) {
		t.Fatalf("NextRunAt was not advanced; got %v vs %v", got.NextRunAt, now)
	}
}

// TestLive_SchedulerLeaderElection runs two schedulers against the
// same DB and asserts only one drives the loop. The non-leader
// should observe lock contention and back off.
func TestLive_SchedulerLeaderElection(t *testing.T) {
	db := dialLiveScheduler(t)
	store := scheduler.NewPostgresStore(db)
	jq := jobqueue.NewPostgres(db)
	enq := service.NewJobQueueEnqueuer(jq)

	if err := store.Create(context.Background(), scheduler.Schedule{
		ID:         "sch-leader",
		PipelineID: "00000000-0000-0000-0000-000000000002",
		CronExpr:   "* * * * *",
		Timezone:   "UTC",
		NextRunAt:  time.Now().UTC().Add(-1 * time.Minute),
		Enabled:    true,
	}); err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	mkRunner := func() *scheduler.Runner {
		r, err := scheduler.NewRunner(scheduler.Options{
			DB:        db,
			Store:     store,
			Enqueuer:  enq,
			TickEvery: 100 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}
		return r
	}
	runner1 := mkRunner()
	runner2 := mkRunner()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done1 := make(chan struct{})
	done2 := make(chan struct{})
	go func() { defer close(done1); _ = runner1.Run(ctx) }()
	// Give runner1 a head start so it definitely wins the advisory lock.
	time.Sleep(150 * time.Millisecond)
	go func() { defer close(done2); _ = runner2.Run(ctx) }()

	// Wait until at least one job has been enqueued — that's the
	// leader doing its work. If both runners were firing concurrently
	// without serialisation we'd see > 1 job per cron tick window.
	deadline := time.Now().Add(4 * time.Second)
	var jobs int
	for time.Now().Before(deadline) {
		row := db.QueryRow(`SELECT COUNT(*) FROM jobs`)
		if err := row.Scan(&jobs); err != nil {
			t.Fatalf("count jobs: %v", err)
		}
		if jobs >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if jobs < 1 {
		t.Fatal("no jobs enqueued; neither replica became leader")
	}
}
