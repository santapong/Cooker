package server

import (
	"context"
	"testing"
	"time"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/store/memory"
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
