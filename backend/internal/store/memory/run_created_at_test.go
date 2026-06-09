package memory

import (
	"context"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// TestRunStore_ListOrderedByCreatedAt verifies that List returns runs in
// newest-first order, matching the Postgres ORDER BY created_at DESC behaviour.
func TestRunStore_ListOrderedByCreatedAt(t *testing.T) {
	st := New()
	ctx := context.Background()

	pipelineID := "pipe-order-test"
	if err := st.Pipelines.Create(ctx, &model.Pipeline{ID: pipelineID, Name: "order-test"}); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}

	base := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	oldest := &model.PipelineRun{ID: "run-oldest", PipelineID: pipelineID, Status: model.RunStatusSuccess, CreatedAt: base}
	middle := &model.PipelineRun{ID: "run-middle", PipelineID: pipelineID, Status: model.RunStatusSuccess, CreatedAt: base.Add(time.Minute)}
	newest := &model.PipelineRun{ID: "run-newest", PipelineID: pipelineID, Status: model.RunStatusSuccess, CreatedAt: base.Add(2 * time.Minute)}

	// Insert in arbitrary order to confirm List doesn't rely on insertion order.
	for _, r := range []*model.PipelineRun{middle, oldest, newest} {
		if err := st.Runs.Create(ctx, r); err != nil {
			t.Fatalf("create run %s: %v", r.ID, err)
		}
	}

	got, err := st.Runs.List(ctx, pipelineID, 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d runs, want 3", len(got))
	}

	wantOrder := []string{"run-newest", "run-middle", "run-oldest"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}

	// Also verify that CreatedAt is preserved through the round-trip.
	if !got[0].CreatedAt.Equal(newest.CreatedAt) {
		t.Errorf("got[0].CreatedAt = %v, want %v", got[0].CreatedAt, newest.CreatedAt)
	}
}

// TestRunStore_CreateSetsCreatedAt verifies that Create stamps CreatedAt when
// the caller leaves it at the zero value.
func TestRunStore_CreateSetsCreatedAt(t *testing.T) {
	st := New()
	ctx := context.Background()

	pipelineID := "pipe-stamp-test"
	if err := st.Pipelines.Create(ctx, &model.Pipeline{ID: pipelineID, Name: "stamp-test"}); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}

	before := time.Now()
	run := &model.PipelineRun{ID: "run-stamp", PipelineID: pipelineID, Status: model.RunStatusPending}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}
	after := time.Now()

	if run.CreatedAt.IsZero() {
		t.Fatal("CreatedAt was not set by Create")
	}
	if run.CreatedAt.Before(before) || run.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v is outside [%v, %v]", run.CreatedAt, before, after)
	}
}
