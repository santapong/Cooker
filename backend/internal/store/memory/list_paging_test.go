package memory

// O3 pagination contract tests: limit <= 0 = all, offset slices after
// the store's stable sort, out-of-range offset yields an empty page.
// The pipelines store stands in for all four paged entity stores —
// they share the paginate helper; sort keys are covered per-impl.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func TestPipelines_ListPaging(t *testing.T) {
	st := New()
	ctx := context.Background()

	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := st.Pipelines.Create(ctx, &model.Pipeline{
			ID:        fmt.Sprintf("p%d", i),
			Name:      fmt.Sprintf("p%d", i),
			UpdatedAt: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// limit <= 0 = everything (internal callers pass 0,0).
	all, err := st.Pipelines.List(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("unbounded list: got %d, want 5", len(all))
	}
	// Stable order: updated_at DESC → p4 first.
	if all[0].ID != "p4" || all[4].ID != "p0" {
		t.Errorf("order: got %s..%s, want p4..p0", all[0].ID, all[4].ID)
	}

	page, err := st.Pipelines.List(ctx, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].ID != "p4" || page[1].ID != "p3" {
		t.Errorf("page 1 (limit=2): got %v", pipelineIDs(page))
	}

	page, err = st.Pipelines.List(ctx, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].ID != "p2" || page[1].ID != "p1" {
		t.Errorf("page 2 (limit=2 offset=2): got %v", pipelineIDs(page))
	}

	// Offset past the end is an empty page, not an error.
	page, err = st.Pipelines.List(ctx, 2, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 0 {
		t.Errorf("offset past end: got %d rows, want 0", len(page))
	}

	// Negative offset behaves like 0.
	page, err = st.Pipelines.List(ctx, 1, -3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].ID != "p4" {
		t.Errorf("negative offset: got %v", pipelineIDs(page))
	}
}

func pipelineIDs(ps []*model.Pipeline) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}
