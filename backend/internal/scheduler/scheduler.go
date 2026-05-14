package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/service"
)

// schedulerLockKey is the bigint passed to pg_advisory_lock so a
// single replica drives the loop at a time. Stable constant so a
// rolling deploy hands off between replicas without redefining the
// lock identity.
const schedulerLockKey int64 = 0x434f4f4b45525343 // "COOKERSC"

// Runner is the scheduler tick loop. It acquires the advisory lock,
// scans the schedules table for due rows, enqueues a pipeline-run
// job per due schedule, and bumps next_run_at. On lock loss it
// backs off and retries; on ctx cancel it returns cleanly.
type Runner struct {
	db          *sql.DB
	store       Store
	enqueuer    *service.JobQueueEnqueuer
	tickEvery   time.Duration
	nowFn       func() time.Time
}

// Options configures a Runner.
type Options struct {
	DB        *sql.DB
	Store     Store
	Enqueuer  *service.JobQueueEnqueuer
	// TickEvery is the wakeup interval when this replica holds the
	// lock. Default 30s. Smaller values catch overdue schedules
	// faster at the cost of more queries.
	TickEvery time.Duration
	// NowFn lets tests inject a fake clock. Default time.Now.
	NowFn func() time.Time
}

// NewRunner constructs a Runner. db is required (for the advisory
// lock); store may be a Postgres or Memory implementation. enqueuer
// is required — the scheduler exists to enqueue runs.
func NewRunner(opts Options) (*Runner, error) {
	if opts.DB == nil {
		return nil, fmt.Errorf("scheduler: DB is required")
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("scheduler: Store is required")
	}
	if opts.Enqueuer == nil {
		return nil, fmt.Errorf("scheduler: Enqueuer is required")
	}
	tick := opts.TickEvery
	if tick <= 0 {
		tick = 30 * time.Second
	}
	nowFn := opts.NowFn
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Runner{
		db:        opts.DB,
		store:     opts.Store,
		enqueuer:  opts.Enqueuer,
		tickEvery: tick,
		nowFn:     nowFn,
	}, nil
}

// Run blocks until ctx is cancelled. Acquires the advisory lock,
// ticks while held, releases on cancel. If the lock can't be
// acquired (another replica holds it) Run waits and retries every
// tickEvery * 2 — the leader's heartbeat through SELECT keeps the
// lock alive on its session.
func (r *Runner) Run(ctx context.Context) error {
	slog.Info("scheduler: starting", "tick_every", r.tickEvery.String())
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, acquired, err := r.tryAcquireLock(ctx)
		if err != nil {
			slog.Warn("scheduler: acquire lock failed", "err", err)
			r.sleep(ctx, r.tickEvery*2)
			continue
		}
		if !acquired {
			// Another replica holds the lock. Sleep and retry; the
			// existing leader's session-scoped lock will release
			// automatically when it exits or dies.
			if conn != nil {
				_ = conn.Close()
			}
			r.sleep(ctx, r.tickEvery*2)
			continue
		}
		slog.Info("scheduler: acquired leader lock")
		r.leadLoop(ctx, conn)
		// leadLoop returns when ctx cancels OR the lock check fails.
		// In both cases the conn is closed inside leadLoop.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("scheduler: lost leader lock; retrying")
	}
}

// tryAcquireLock attempts to acquire the session-scoped advisory
// lock on a dedicated connection. Returns (conn, true, nil) on
// success, (nil, false, nil) when the lock is held elsewhere, or an
// error on a real DB failure.
func (r *Runner) tryAcquireLock(ctx context.Context) (*sql.Conn, bool, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	var got bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, schedulerLockKey).Scan(&got); err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	if !got {
		return conn, false, nil
	}
	return conn, true, nil
}

// leadLoop is the active-leader path. Closes the conn (releasing the
// lock) on return.
func (r *Runner) leadLoop(ctx context.Context, conn *sql.Conn) {
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, schedulerLockKey)
		_ = conn.Close()
	}()
	ticker := time.NewTicker(r.tickEvery)
	defer ticker.Stop()
	// Fire one tick immediately so a fresh leader doesn't wait the
	// full interval on its first wakeup.
	r.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Lock-liveness probe: a network blip can drop the
			// session without us noticing. A cheap SELECT keeps the
			// connection warm and surfaces a dead one fast.
			if err := conn.PingContext(ctx); err != nil {
				slog.Warn("scheduler: leader conn ping failed; releasing", "err", err)
				return
			}
			r.tick(ctx)
		}
	}
}

// tick fetches due schedules, enqueues a run per schedule, and
// updates next_run_at. Errors are logged but do not stop the loop
// — we'd rather miss one tick than wedge the whole scheduler.
func (r *Runner) tick(ctx context.Context) {
	now := r.nowFn()
	due, err := r.store.DueBefore(ctx, now)
	if err != nil {
		slog.Warn("scheduler: due-before failed", "err", err)
		return
	}
	for _, s := range due {
		r.fire(ctx, s, now)
	}
}

func (r *Runner) fire(ctx context.Context, s Schedule, now time.Time) {
	cron, err := Parse(s.CronExpr)
	if err != nil {
		slog.Warn("scheduler: bad cron; disabling row", "id", s.ID, "err", err)
		// Disable the row so we don't keep hammering it. Operator can
		// fix the expression and re-enable.
		s.Enabled = false
		_ = r.store.Update(ctx, s)
		return
	}
	// Synthesise a run ID up front so MarkFired records it even if
	// the executor hasn't run yet. The actual PipelineRun row will
	// be created by JobQueueRunner when the worker picks up the job.
	runID := newRunID()
	if err := r.enqueuer.EnqueueRun(ctx, s.PipelineID, runID); err != nil {
		slog.Warn("scheduler: enqueue failed", "schedule", s.ID, "err", err)
		// Don't update next_run_at — retry on the next tick.
		return
	}
	next, err := cron.Next(now, s.LoadLocation())
	if err != nil {
		slog.Warn("scheduler: next-after-fire failed", "id", s.ID, "err", err)
		return
	}
	if err := r.store.MarkFired(ctx, s.ID, now, runID, next); err != nil {
		slog.Warn("scheduler: mark-fired failed", "id", s.ID, "err", err)
	}
}

func (r *Runner) sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// newRunID returns a synthesised pipeline-run ID. Same shape as the
// jobqueue's job IDs so logs are visually consistent.
var newRunID = func() string {
	return fmt.Sprintf("sched_%d", time.Now().UnixNano())
}

// Ensure the jobqueue package is referenced so the import isn't
// optimised away when the file is read in isolation by reviewers.
var _ = jobqueue.NotifyChannel
