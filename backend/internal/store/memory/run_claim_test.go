package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestRunClaimRejectsStaleMemoryWritesAndPointerMutation(t *testing.T) {
	st := New()
	ctx := context.Background()
	run := &model.PipelineRun{ID: "r", PipelineID: "p", Status: model.RunStatusPending}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	run.Status = model.RunStatusSuccess // The caller must not alias stored state.
	got, _ := st.Runs.Get(ctx, "r")
	if got.Status != model.RunStatusPending {
		t.Fatal("Create retained mutable caller pointer")
	}
	claim := store.WithRunClaim(ctx, "j", "old", func(context.Context, func() error) error { return store.ErrConflict })
	for name, write := range map[string]func() error{
		"update":    func() error { return st.Runs.Update(claim, run) },
		"progress":  func() error { return st.Runs.UpdateProgress(claim, "r", nil) },
		"status":    func() error { return st.Runs.UpdateStatus(claim, "r", model.RunStatusSuccess, nil, "") },
		"heartbeat": func() error { return st.Runs.UpdateHeartbeat(claim, "r", time.Now()) },
		"history":   func() error { return st.AppDeploys.Create(claim, &model.AppDeploy{ID: "d", RunID: "r"}) },
		"app URL":   func() error { return st.Apps.UpdateDeployedURL(claim, "a", "https://stale.example") },
	} {
		if err := write(); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("%s stale write accepted: %v", name, err)
		}
	}
	got, _ = st.Runs.Get(ctx, "r")
	if got.Status != model.RunStatusPending {
		t.Fatal("fenced write changed stored state")
	}
}
