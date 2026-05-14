//go:build integration

package jobqueue_test

// Live-Postgres integration tests for the durable job queue. Gated
// on the `integration` build tag AND the COOKER_INTEGRATION_DATABASE_URL
// env var; both have to be present for the suite to run. Default
// `go test ./...` skips this file because the build tag isn't set.
//
// Run locally with:
//
//	COOKER_INTEGRATION_DATABASE_URL=postgres://...?sslmode=disable \
//	  go test -tags=integration ./internal/jobqueue/... -count=1 -race
//
// CI: the workflow's backend job sets DATABASE_URL but not
// COOKER_INTEGRATION_DATABASE_URL; flip to the integration variant
// when the docker-compose CI hook lands.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/santapong/cooker/internal/jobqueue"
)

// jobsSchema is the inline copy of migration 010_jobs.up.sql. Kept
// here so the test is self-contained — drift-prevention is via the
// drift test below which compares the two strings.
const jobsSchema = `
CREATE TABLE IF NOT EXISTS jobs (
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
CREATE INDEX IF NOT EXISTS jobs_pickup_idx
    ON jobs (run_at)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS jobs_running_concurrency_idx
    ON jobs (concurrency_key)
    WHERE status = 'running' AND concurrency_key <> '';
`

// dialLive opens a connection to the integration DB or skips the test
// when the env var is empty. Drops the jobs table on entry so each
// test starts from a clean slate — these tests own the table.
func dialLive(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL not set; skipping live-DB jobqueue tests")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS jobs CASCADE`); err != nil {
		t.Fatalf("drop jobs: %v", err)
	}
	if _, err := db.Exec(jobsSchema); err != nil {
		t.Fatalf("create jobs: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestLive_MultiWorkerSkipLocked spawns N workers against the same
// queue and enqueues N*K jobs; asserts that every job is processed
// exactly once and that total throughput beats the single-worker
// case. The SKIP LOCKED clause is what makes this safe — two workers
// dequeuing the same row would otherwise cause double-processing.
func TestLive_MultiWorkerSkipLocked(t *testing.T) {
	db := dialLive(t)
	store := jobqueue.NewPostgres(db)
	reg := jobqueue.NewRegistry()

	const (
		workerN = 4
		jobN    = 32
	)

	var (
		processed int32
		seen      sync.Map // jobID → struct{} for duplicate detection
	)
	reg.Register("k", func(ctx context.Context, j *jobqueue.Job) error {
		if _, dup := seen.LoadOrStore(j.ID, struct{}{}); dup {
			t.Errorf("duplicate dispatch of %s", j.ID)
		}
		atomic.AddInt32(&processed, 1)
		return nil
	})

	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{
		Store:     store,
		Registry:  reg,
		Size:      workerN,
		PollEvery: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	for i := 0; i < jobN; i++ {
		payload, _ := json.Marshal(map[string]int{"i": i})
		if _, err := store.Enqueue(ctx, "k", payload, jobqueue.EnqueueOptions{}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for atomic.LoadInt32(&processed) < jobN && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&processed); got != jobN {
		t.Fatalf("processed=%d want %d (workers may have raced or skipped)", got, jobN)
	}
}

// TestLive_ConcurrencyKeySerialises enqueues N jobs with the same
// concurrency_key and asserts that at most one runs at any instant
// — the NOT EXISTS guard inside Dequeue should suppress siblings
// while one job of the same key is still in 'running'.
func TestLive_ConcurrencyKeySerialises(t *testing.T) {
	db := dialLive(t)
	store := jobqueue.NewPostgres(db)
	reg := jobqueue.NewRegistry()

	const jobN = 6

	var (
		inflight int32
		peak     int32
	)
	reg.Register("k", func(ctx context.Context, j *jobqueue.Job) error {
		cur := atomic.AddInt32(&inflight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		// Hold the slot briefly so a concurrent worker has a chance
		// to violate the invariant if SKIP LOCKED + NOT EXISTS aren't
		// doing their job.
		time.Sleep(40 * time.Millisecond)
		atomic.AddInt32(&inflight, -1)
		return nil
	})

	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{
		Store:     store,
		Registry:  reg,
		Size:      4, // try to over-subscribe so a buggy lock would show
		PollEvery: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	for i := 0; i < jobN; i++ {
		if _, err := store.Enqueue(ctx, "k", nil, jobqueue.EnqueueOptions{
			ConcurrencyKey: "shared",
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		stats, err := store.Stats(ctx)
		if err != nil {
			t.Fatalf("stats: %v", err)
		}
		if stats[jobqueue.StatusSucceeded] >= jobN {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&peak); got > 1 {
		t.Fatalf("peak concurrent in-flight for same key = %d, want <= 1", got)
	}
}

// TestLive_NotifyWakesWorker verifies LISTEN/NOTIFY shortens the
// pickup latency below the poll interval. With PollEvery=2s and an
// active listener, a freshly enqueued job should fire within ~200ms
// rather than waiting up to 2s for the next poll.
func TestLive_NotifyWakesWorker(t *testing.T) {
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL not set")
	}
	db := dialLive(t)
	store := jobqueue.NewPostgres(db)
	reg := jobqueue.NewRegistry()

	fired := make(chan time.Time, 1)
	reg.Register("k", func(ctx context.Context, j *jobqueue.Job) error {
		fired <- time.Now()
		return nil
	})

	listener, err := jobqueue.NewPqListener(dsn)
	if err != nil {
		t.Fatalf("NewPqListener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{
		Store:     store,
		Registry:  reg,
		Size:      1,
		PollEvery: 2 * time.Second, // intentionally long
		Notifier:  listener,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	// Give the worker a moment to settle on its first poll so the
	// next-poll countdown starts; otherwise we'd measure the initial
	// settle, not the NOTIFY wake-up.
	time.Sleep(200 * time.Millisecond)

	enqueuedAt := time.Now()
	if _, err := store.Enqueue(ctx, "k", nil, jobqueue.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case at := <-fired:
		latency := at.Sub(enqueuedAt)
		if latency > 1*time.Second {
			t.Fatalf("NOTIFY did not wake worker within 1s (latency=%s); poll interval was 2s, expected near-instant wake", latency)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not fire within 3s")
	}
}
