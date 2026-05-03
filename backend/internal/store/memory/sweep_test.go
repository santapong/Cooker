package memory

import (
	"context"
	"testing"
	"time"

	"github.com/cooker-ci/cooker/internal/model"
)

func TestSweepOrphans_FlagsStaleAndMissingHeartbeats(t *testing.T) {
	st := New()
	ctx := context.Background()
	now := time.Now()

	pipelineID := "p1"
	if err := st.Pipelines.Create(ctx, &model.Pipeline{ID: pipelineID, Name: "p"}); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}

	fresh := &model.PipelineRun{ID: "fresh", PipelineID: pipelineID, Status: model.RunStatusRunning}
	stale := &model.PipelineRun{ID: "stale", PipelineID: pipelineID, Status: model.RunStatusRunning}
	missing := &model.PipelineRun{ID: "missing", PipelineID: pipelineID, Status: model.RunStatusRunning}
	notRunning := &model.PipelineRun{ID: "done", PipelineID: pipelineID, Status: model.RunStatusSuccess}

	for _, r := range []*model.PipelineRun{fresh, stale, missing, notRunning} {
		if err := st.Runs.Create(ctx, r); err != nil {
			t.Fatalf("seed run %s: %v", r.ID, err)
		}
	}

	freshTs := now.Add(-5 * time.Second)
	staleTs := now.Add(-10 * time.Minute)
	if err := st.Runs.UpdateHeartbeat(ctx, "fresh", freshTs); err != nil {
		t.Fatalf("hb fresh: %v", err)
	}
	if err := st.Runs.UpdateHeartbeat(ctx, "stale", staleTs); err != nil {
		t.Fatalf("hb stale: %v", err)
	}
	// "missing" deliberately gets no heartbeat.

	swept, err := st.Runs.SweepOrphans(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if swept != 2 {
		t.Fatalf("swept = %d, want 2 (stale + missing)", swept)
	}

	got, _ := st.Runs.Get(ctx, "fresh")
	if got.Status != model.RunStatusRunning {
		t.Errorf("fresh: status = %s, want still running", got.Status)
	}
	got, _ = st.Runs.Get(ctx, "stale")
	if got.Status != model.RunStatusFailed || got.Error == "" {
		t.Errorf("stale: status = %s, error = %q; expected failed with error", got.Status, got.Error)
	}
	got, _ = st.Runs.Get(ctx, "missing")
	if got.Status != model.RunStatusFailed {
		t.Errorf("missing: status = %s, want failed", got.Status)
	}
	got, _ = st.Runs.Get(ctx, "done")
	if got.Status != model.RunStatusSuccess {
		t.Errorf("done: status = %s, want success (untouched)", got.Status)
	}
}
