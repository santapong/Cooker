package server

import (
	"context"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestRunCoordinator_HeartbeatsRunRow(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	if err := st.Pipelines.Create(ctx, &model.Pipeline{ID: "p"}); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	run := &model.PipelineRun{ID: "r1", PipelineID: "p", Status: model.RunStatusRunning}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	rc := NewRunCoordinator(st)
	done := make(chan struct{})
	rc.Spawn(ctx, run.ID, func(ctx context.Context) error {
		// Hold long enough that the initial heartbeat lands.
		time.Sleep(50 * time.Millisecond)
		close(done)
		return nil
	})
	<-done
	rc.Wait(context.Background())

	got, err := st.Runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.HeartbeatAt == nil {
		t.Fatal("heartbeat_at should have been written")
	}
	if time.Since(*got.HeartbeatAt) > time.Second {
		t.Fatalf("heartbeat is suspiciously old: %s ago", time.Since(*got.HeartbeatAt))
	}
}

// S26-05-23 follow-up: orphan threshold is configurable via env.
// We can't easily re-init the package var post-load, but we can lock
// the invariants the loader cares about: the default exceeds the
// heartbeat (so a single missed tick doesn't false-positive) and
// matches the documented 60s default.
func TestOrphanThreshold_DefaultIsSafe(t *testing.T) {
	if orphanThreshold != defaultOrphanSweepInterval {
		t.Errorf("orphanThreshold=%s; expected default %s (env override leaking in test environment?)",
			orphanThreshold, defaultOrphanSweepInterval)
	}
	if orphanThreshold <= heartbeatInterval {
		t.Fatalf("orphanThreshold=%s must exceed heartbeatInterval=%s",
			orphanThreshold, heartbeatInterval)
	}
}

// TestRunCoordinator_F07_HeartbeatSucceedsWhenRowCreatedBeforeSpawn asserts
// the F-07 fix (docs/audits/2026-05-store-parity.md §F-07): the heartbeat
// written by Spawn's first tick must succeed when the row exists before Spawn
// is called. Prior to the fix, app-deploy runs never created the row first,
// so heartbeatBestEffort silently swallowed ErrNotFound on every tick.
// This test creates the stub row *before* Spawn (matching the fixed
// handler/app.go behaviour) and verifies that the heartbeat_at column is
// populated — confirming that ErrNotFound no longer occurs on this path.
func TestRunCoordinator_F07_HeartbeatSucceedsWhenRowCreatedBeforeSpawn(t *testing.T) {
	t.Parallel()
	st := memory.New()
	ctx := context.Background()

	// Simulate the app's synthetic pipelineID and the stub run row that
	// handler/app.go now creates before calling Spawn (F-07 fix).
	appID := "app-f07"
	runID := "run-f07"
	now := time.Now()
	stub := &model.PipelineRun{
		ID:         runID,
		PipelineID: appID,
		Status:     model.RunStatusRunning,
		StartedAt:  &now,
	}
	if err := st.Runs.Create(ctx, stub); err != nil {
		t.Fatalf("create stub run: %v", err)
	}

	rc := NewRunCoordinator(st)
	// heartbeatBestEffort is called synchronously by Spawn before work
	// starts. Give the test goroutine a moment, then return.
	done := make(chan struct{})
	rc.Spawn(ctx, runID, func(ctx context.Context) error {
		time.Sleep(20 * time.Millisecond)
		close(done)
		return nil
	})
	<-done
	rc.Wait(context.Background())

	got, err := st.Runs.Get(ctx, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	// If F-07 regression reappears (row missing at heartbeat time) this
	// assertion fails because heartbeatBestEffort silently discards the
	// ErrNotFound and heartbeat_at stays nil.
	if got.HeartbeatAt == nil {
		t.Fatal("F-07: heartbeat_at is nil; row was not found when the coordinator wrote its first heartbeat — stub row must be created before Spawn")
	}
	if time.Since(*got.HeartbeatAt) > 5*time.Second {
		t.Fatalf("F-07: heartbeat_at is suspiciously stale (%s ago)", time.Since(*got.HeartbeatAt))
	}
}

func TestRunCoordinator_DrainsOnWait(t *testing.T) {
	st := memory.New()
	rc := NewRunCoordinator(st)
	finished := make(chan struct{})
	rc.Spawn(context.Background(), "r2", func(ctx context.Context) error {
		time.Sleep(30 * time.Millisecond)
		close(finished)
		return nil
	})

	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rc.Wait(waitCtx)

	select {
	case <-finished:
	default:
		t.Fatal("Wait returned before goroutine finished")
	}
}

// SpawnWithDeadline must cancel the work context once the per-run
// deadline elapses (the per-pipeline RunDeadline override path).
func TestRunCoordinator_SpawnWithDeadline_CancelsWork(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	run := &model.PipelineRun{ID: "r-deadline", PipelineID: "p", Status: model.RunStatusRunning}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	rc := NewRunCoordinator(st)
	got := make(chan error, 1)
	rc.SpawnWithDeadline(ctx, run.ID, 50*time.Millisecond, func(workCtx context.Context) error {
		select {
		case <-workCtx.Done():
			got <- workCtx.Err()
		case <-time.After(5 * time.Second):
			got <- nil
		}
		return nil
	})
	rc.Wait(context.Background())

	select {
	case err := <-got:
		if err == nil {
			t.Fatal("work ctx should have been cancelled by the 50ms deadline")
		}
	default:
		t.Fatal("work never reported")
	}
}
