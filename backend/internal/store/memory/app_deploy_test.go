package memory

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestAppDeployStore_ListNewestFirstAndLimit(t *testing.T) {
	st := New()
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := st.AppDeploys.Create(ctx, &model.AppDeploy{
			ID: fmt.Sprintf("d%d", i), AppID: "a1", RunID: fmt.Sprintf("r%d", i),
			ImageRef: fmt.Sprintf("reg/app:%d", i), Status: model.RunStatusSuccess,
			Kind: model.AppDeployKindDeploy, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Different app — must not leak into a1's history.
	_ = st.AppDeploys.Create(ctx, &model.AppDeploy{ID: "other", AppID: "a2", RunID: "r"})

	got, err := st.AppDeploys.ListByApp(ctx, "a1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("limit not applied: got %d rows", len(got))
	}
	if got[0].ID != "d4" || got[1].ID != "d3" || got[2].ID != "d2" {
		t.Errorf("not newest-first: %s %s %s", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestAppDeployStore_GetNotFound(t *testing.T) {
	st := New()
	if _, err := st.AppDeploys.Get(context.Background(), "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
