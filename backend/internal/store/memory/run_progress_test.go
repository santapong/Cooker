package memory

// Tests for the O2 (run-JSONB bloat) store additions: GetSummary must
// strip per-stage logs without disturbing what Get returns, and
// UpdateProgress must persist stage statuses log-free while leaving the
// run's lifecycle fields (status/error/finished_at) alone. Parity
// contract with the Postgres impl (jsonb_agg(elem - 'logs') projection
// and the single-column stage_runs UPDATE).

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestRuns_GetSummary_StripsLogsButGetKeepsThem(t *testing.T) {
	st := New()
	ctx := context.Background()

	if err := st.Runs.Create(ctx, &model.PipelineRun{
		ID:         "run-1",
		PipelineID: "pipe",
		Status:     model.RunStatusRunning,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusSuccess, Logs: "big build log"},
			{StageID: "push", Status: model.RunStatusRunning, Logs: "push in progress"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	sum, err := st.Runs.GetSummary(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if len(sum.StageRuns) != 2 {
		t.Fatalf("summary stage count: got %d, want 2", len(sum.StageRuns))
	}
	for _, sr := range sum.StageRuns {
		if sr.Logs != "" {
			t.Errorf("stage %s: summary kept logs %q", sr.StageID, sr.Logs)
		}
	}
	// Statuses survive the strip — the summary is still useful for polling.
	if sum.StageRuns[0].Status != model.RunStatusSuccess || sum.StageRuns[1].Status != model.RunStatusRunning {
		t.Errorf("summary lost stage statuses: %+v", sum.StageRuns)
	}

	// The strip must not leak back into the stored run: full Get still
	// serves the logs (the stage-logs endpoint depends on this).
	full, err := st.Runs.Get(ctx, "run-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if full.StageRuns[0].Logs != "big build log" {
		t.Errorf("Get after GetSummary lost logs: %q", full.StageRuns[0].Logs)
	}

	if _, err := st.Runs.GetSummary(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetSummary(missing) = %v, want ErrNotFound", err)
	}
}

func TestRuns_UpdateProgress_PersistsStatusesWithoutLogs(t *testing.T) {
	st := New()
	ctx := context.Background()

	if err := st.Runs.Create(ctx, &model.PipelineRun{
		ID:         "run-1",
		PipelineID: "pipe",
		Status:     model.RunStatusRunning,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusPending},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// The executor's flush passes its live copy WITH logs; the store must
	// persist statuses but drop the log text (logs land once, in the
	// terminal full Update).
	if err := st.Runs.UpdateProgress(ctx, "run-1", []model.StageRun{
		{StageID: "build", Status: model.RunStatusSuccess, Logs: "should not persist"},
		{StageID: "push", Status: model.RunStatusRunning, Logs: "should not persist either"},
	}); err != nil {
		t.Fatalf("UpdateProgress: %v", err)
	}

	got, err := st.Runs.Get(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.StageRuns) != 2 {
		t.Fatalf("stage count after progress flush: got %d, want 2", len(got.StageRuns))
	}
	if got.StageRuns[0].Status != model.RunStatusSuccess || got.StageRuns[1].Status != model.RunStatusRunning {
		t.Errorf("statuses not persisted: %+v", got.StageRuns)
	}
	for _, sr := range got.StageRuns {
		if sr.Logs != "" {
			t.Errorf("stage %s: progress flush persisted logs %q", sr.StageID, sr.Logs)
		}
	}
	// Lifecycle fields untouched — UpdateProgress is stage_runs-only.
	if got.Status != model.RunStatusRunning {
		t.Errorf("run status changed by UpdateProgress: %s", got.Status)
	}

	if err := st.Runs.UpdateProgress(ctx, "nope", nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdateProgress(missing) = %v, want ErrNotFound", err)
	}
}
