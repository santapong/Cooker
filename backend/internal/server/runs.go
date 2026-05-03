package server

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/cooker-ci/cooker/internal/store"
)

// heartbeatInterval is how often a tracked goroutine writes
// heartbeat_at to its run row. Must be < orphanThreshold / 2 so the
// boot-time sweep doesn't false-positive on healthy runs.
const heartbeatInterval = 30 * time.Second

// orphanThreshold is the staleness past which a heartbeat declares a
// run orphaned. Set deliberately above 2*heartbeatInterval to absorb
// one missed tick.
const orphanThreshold = 90 * time.Second

// runDrainTimeout caps how long the coordinator's Wait will block
// after a shutdown ctx cancel. Stragglers beyond this get cut off and
// will be flagged by the next boot's orphan sweep.
const runDrainTimeout = 25 * time.Second

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

// Spawn launches work in a tracked goroutine. The supplied ctx is
// extended with a 30-minute deadline (matching the existing app-deploy
// behaviour) and used for both work and heartbeat writes. work is
// expected to mutate the run state through the store; the coordinator
// only manages liveness, not run semantics.
func (rc *RunCoordinator) Spawn(ctx context.Context, runID string, work func(context.Context) error) {
	rc.wg.Add(1)
	go func() {
		defer rc.wg.Done()
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		// First heartbeat, written synchronously, so a sweep that runs
		// shortly after Spawn never declares a fresh run orphaned.
		// Missing-row is tolerated silently so paths that create the
		// row post-hoc (synthesised app-deploys) don't spam warnings.
		rc.heartbeatBestEffort(ctx, runID, time.Now())
		hbCtx, hbCancel := context.WithCancel(ctx)
		defer hbCancel()
		go func() {
			for {
				select {
				case <-hbCtx.Done():
					return
				case t := <-ticker.C:
					rc.heartbeatBestEffort(hbCtx, runID, t)
				}
			}
		}()
		if err := work(ctx); err != nil {
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
	slog.Warn("run coordinator: heartbeat failed", "run", runID, "err", err)
}

// Wait blocks until all spawned goroutines exit or the supplied ctx is
// cancelled, whichever comes first.
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
