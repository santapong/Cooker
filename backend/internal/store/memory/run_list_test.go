package memory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// seedRuns inserts n runs for pipelineID with ascending CreatedAt and a
// non-empty per-stage log, returning the store.
func seedRuns(t *testing.T, n int, pipelineID string) *store.Store {
	t.Helper()
	st := New()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		r := &model.PipelineRun{
			ID:         fmt.Sprintf("run-%02d", i),
			PipelineID: pipelineID,
			Status:     model.RunStatusSuccess,
			CreatedAt:  base.Add(time.Duration(i) * time.Minute),
			StageRuns: []model.StageRun{
				{StageID: "build", Status: model.RunStatusSuccess, Logs: "line one\nline two\n"},
			},
		}
		if err := st.Runs.Create(context.Background(), r); err != nil {
			t.Fatalf("create %s: %v", r.ID, err)
		}
	}
	return st
}

func TestRunList_LimitOffset(t *testing.T) {
	st := seedRuns(t, 5, "pipe-1")
	ctx := context.Background()

	// limit only: newest two.
	got, err := st.Runs.List(ctx, "pipe-1", 2, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != "run-04" || got[1].ID != "run-03" {
		t.Fatalf("limit=2 returned wrong page: %+v", ids(got))
	}

	// offset into the middle.
	got, err = st.Runs.List(ctx, "pipe-1", 2, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != "run-02" || got[1].ID != "run-01" {
		t.Fatalf("limit=2 offset=2 returned wrong page: %+v", ids(got))
	}

	// offset past the end: empty, not error.
	got, err = st.Runs.List(ctx, "pipe-1", 2, 99)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("offset past end should be empty, got %+v", ids(got))
	}

	// limit <= 0: full history (legacy behaviour).
	got, err = st.Runs.List(ctx, "pipe-1", 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("limit=0 should return all 5, got %d", len(got))
	}
}

func TestRunList_StripsLogsWithoutMutatingStore(t *testing.T) {
	st := seedRuns(t, 1, "pipe-1")
	ctx := context.Background()

	got, err := st.Runs.List(ctx, "pipe-1", 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || len(got[0].StageRuns) != 1 {
		t.Fatalf("unexpected shape: %+v", got)
	}
	if got[0].StageRuns[0].Logs != "" {
		t.Fatalf("List must omit per-stage Logs, got %q", got[0].StageRuns[0].Logs)
	}

	// The stored row must keep its logs: Get returns them in full.
	full, err := st.Runs.Get(ctx, "run-00")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if full.StageRuns[0].Logs != "line one\nline two\n" {
		t.Fatalf("Get lost logs after List: %q", full.StageRuns[0].Logs)
	}
}

func ids(runs []*model.PipelineRun) []string {
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.ID
	}
	return out
}
