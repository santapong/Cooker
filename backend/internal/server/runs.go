package server

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/observability"
	"github.com/santapong/cooker/internal/store"
)

// heartbeatInterval is how often a tracked goroutine writes
// heartbeat_at to its run row. Must be < orphanThreshold so the
// boot-time sweep doesn't false-positive on healthy runs.
const heartbeatInterval = 30 * time.Second

// defaultOrphanSweepInterval is the staleness past which a heartbeat
// declares a run orphaned at boot. Operators can override via
// COOKER_ORPHAN_SWEEP_INTERVAL (Go duration string) when their
// workload tolerates a different staleness window. Default 60s
// (must remain > heartbeatInterval to avoid false-positives on
// healthy runs that miss a single tick).
const defaultOrphanSweepInterval = 60 * time.Second

// orphanThreshold is the staleness used by the boot-time sweep.
// Initialised from COOKER_ORPHAN_SWEEP_INTERVAL; falls back to the
// default when unset, malformed, or <= heartbeatInterval (a smaller
// value would reap healthy in-flight runs).
var orphanThreshold = func() time.Duration {
	if v := os.Getenv("COOKER_ORPHAN_SWEEP_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > heartbeatInterval {
			return d
		}
	}
	return defaultOrphanSweepInterval
}()

// runDrainTimeout caps how long the coordinator's Wait will block
// after a shutdown ctx cancel. Stragglers beyond this get cut off and
// will be flagged by the next boot's orphan sweep.
const runDrainTimeout = 25 * time.Second

// runDeadline is the upper bound on how long Spawn lets work run
// before its context is force-cancelled. Defaults to 30 minutes —
// long enough for realistic Kaniko builds — but operators with
// genuinely long monorepo builds can override via the
// COOKER_RUN_DEADLINE env var (Go duration string, e.g. "2h").
// Setting 0 or a negative value falls back to the default.
var runDeadline = func() time.Duration {
	if v := os.Getenv("COOKER_RUN_DEADLINE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Minute
}()

// RunCoordinator tracks in-flight pipeline-run goroutines so they can
// be heart-beaten and drained on shutdown.
type RunCoordinator struct {
	wg    sync.WaitGroup
	store *store.Store
}

// NewRunCoordinator builds a coordinator wired against the given store.
func NewRunCoordinator(st *store.Store) *RunCoordinator {
	return &RunCoordinator{store: st}
}

// Spawn launches work with the cluster-default deadline
// (COOKER_RUN_DEADLINE, 30 minutes when unset). See SpawnWithDeadline.
func (rc *RunCoordinator) Spawn(ctx context.Context, runID string, work func(context.Context) error) {
	rc.SpawnWithDeadline(ctx, runID, runDeadline, work)
}

// SpawnWithDeadline launches work in a tracked goroutine with an
// explicit upper bound (per-pipeline RunDeadline override). deadline
// <= 0 falls back to the cluster default. The bounded ctx is used for
// both work and heartbeat writes. work is expected to mutate the run
// state through the store; the coordinator only manages liveness,
// not run semantics.
func (rc *RunCoordinator) SpawnWithDeadline(ctx context.Context, runID string, deadline time.Duration, work func(context.Context) error) {
	if deadline <= 0 {
		deadline = runDeadline
	}
	rc.wg.Add(1)
	go func() {
		defer rc.wg.Done()
		// Apply the upper bound. Without this a stuck builder /
		// kubectl / git push could hold the goroutine forever; the
		// sweep wouldn't catch it because heartbeats keep firing.
		workCtx, cancelWork := context.WithTimeout(ctx, deadline)
		defer cancelWork()
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		// First heartbeat, written synchronously, so a sweep that runs
		// shortly after Spawn never declares a fresh run orphaned.
		// Missing-row is still tolerated silently as a defensive measure;
		// the app-deploy path (handler/app.go F-07 fix) now creates the
		// stub row before calling Spawn so this no-op path should no
		// longer be exercised in normal operation.
		rc.heartbeatBestEffort(workCtx, runID, time.Now())
		hbCtx, hbCancel := context.WithCancel(workCtx)
		hbDone := make(chan struct{})
		go func() {
			defer close(hbDone)
			for {
				select {
				case <-hbCtx.Done():
					return
				case t := <-ticker.C:
					rc.heartbeatBestEffort(hbCtx, runID, t)
				}
			}
		}()
		// Run work, then deterministically tear down the heartbeat
		// goroutine before this outer goroutine returns. Without the
		// join the inner goroutine could outlive the WaitGroup-tracked
		// outer one, missing the final heartbeat and producing a
		// false-positive orphan on the next replica's boot sweep.
		err := work(workCtx)
		hbCancel()
		<-hbDone
		if err != nil {
			slog.Warn("run coordinator: work returned error", "run", runID, "err", err)
		}
	}()
}

// heartbeatBestEffort writes the heartbeat and tolerates "row not yet
// created" silently — that's how synthesised runs (app-deploys)
// behave: the row materialises only when the deployer returns.
func (rc *RunCoordinator) heartbeatBestEffort(ctx context.Context, runID string, ts time.Time) {
	err := rc.store.Runs.UpdateHeartbeat(ctx, runID, ts)
	if err == nil || errors.Is(err, store.ErrNotFound) {
		return
	}
	observability.IncHeartbeatError()
	slog.Warn("run coordinator: heartbeat failed", "run", runID, "err", err)
}

// Wait blocks until all spawned goroutines exit or the supplied ctx is
// cancelled, whichever comes first.
//
// On a drain timeout the inner wg.Wait goroutine continues running until
// the in-flight runs complete naturally — it holds only a pointer to rc
// and terminates without any external intervention once the WaitGroup
// counter reaches zero. This is intentional: the goroutine is
// self-cleaning and bounded in lifetime, so the "leak" is acceptable
// (M / M-Wait-leak). A future refactor could return a <-chan struct{}
// if callers need to observe completion after the timeout.
func (rc *RunCoordinator) Wait(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		rc.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("run coordinator: drain timed out; some runs may be marked orphan on next boot")
	}
}
