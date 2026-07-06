package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// TestRunStore_UpdateStatus verifies UpdateStatus changes only the lifecycle
// columns (status / finished_at / error) and leaves StageRuns and HeartbeatAt
// untouched — the heartbeat-safe, no-full-rewrite property F18/F2 rely on
// (docs/proposals/run-state-concurrency-2026.md).
func TestRunStore_UpdateStatus(t *testing.T) {
	st := New()
	ctx := context.Background()

	hb := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	run := &model.PipelineRun{
		ID:          "run-us",
		PipelineID:  "pipe-us",
		Status:      model.RunStatusRunning,
		StageRuns:   []model.StageRun{{StageID: "s1", Status: model.RunStatusRunning, Logs: "keep me"}},
		HeartbeatAt: &hb,
	}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatalf("create: %v", err)
	}

	fin := time.Date(2026, 6, 26, 12, 5, 0, 0, time.UTC)
	if err := st.Runs.UpdateStatus(ctx, "run-us", model.RunStatusCancelled, &fin, "cancelled by user"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := st.Runs.Get(ctx, "run-us")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != model.RunStatusCancelled {
		t.Errorf("status: got %s, want cancelled", got.Status)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(fin) {
		t.Errorf("finishedAt: got %v, want %v", got.FinishedAt, fin)
	}
	if got.Error != "cancelled by user" {
		t.Errorf("error: got %q, want %q", got.Error, "cancelled by user")
	}
	// Lifecycle-only: the JSONB blobs and heartbeat must be left untouched.
	if len(got.StageRuns) != 1 || got.StageRuns[0].Logs != "keep me" {
		t.Errorf("StageRuns clobbered: %+v", got.StageRuns)
	}
	if got.HeartbeatAt == nil || !got.HeartbeatAt.Equal(hb) {
		t.Errorf("HeartbeatAt clobbered: got %v, want %v", got.HeartbeatAt, hb)
	}
}

// TestRunStore_UpdateStatus_NotFound verifies the ErrNotFound contract.
func TestRunStore_UpdateStatus_NotFound(t *testing.T) {
	st := New()
	err := st.Runs.UpdateStatus(context.Background(), "nope", model.RunStatusCancelled, nil, "")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
