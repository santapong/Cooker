package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

type legacyHistory struct{ store.AppDeployStore }

func TestRuntimeHistoryBulkKeepsAttemptAndRuntimeSeparate(t *testing.T) {
	ctx := context.Background()
	h := memory.New().AppDeploys
	now := time.Now().UTC()
	put := func(d *model.AppDeploy) {
		t.Helper()
		if err := h.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	put(&model.AppDeploy{ID: "runtime", AppID: "app", RunID: "runtime", Kind: model.AppDeployKindDeploy, Status: model.RunStatusSuccess, CreatedAt: now})
	for i := 0; i < 120; i++ {
		put(&model.AppDeploy{ID: fmt.Sprintf("rejected-%03d", i), AppID: "app", RunID: fmt.Sprint(i), Kind: model.AppDeployKindRejected, Status: model.RunStatusFailed, CreatedAt: now.Add(time.Duration(i+1) * time.Second)})
	}
	latest, err := h.(store.CatalogAppDeployReader).LatestByApps(ctx, []string{"app"})
	if err != nil || latest["app"].Kind != model.AppDeployKindRejected {
		t.Fatalf("latest attempt hidden: %+v %v", latest, err)
	}
	got, err := store.LatestRuntimeAttempts(ctx, h, []string{"app", "unknown"})
	if err != nil || got["app"].ID != "runtime" || got["unknown"] != nil {
		t.Fatalf("bounded rejection history hid runtime: %+v %v", got, err)
	}
	got["app"].Status = model.RunStatusFailed
	got, err = store.LatestRuntimeAttempts(ctx, h, []string{"app"})
	if err != nil || got["app"].Status != model.RunStatusSuccess {
		t.Fatal("reader exposed a stored pointer", got, err)
	}
	if _, err = store.LatestRuntimeAttempts(ctx, legacyHistory{h}, []string{"app"}); err == nil {
		t.Fatal("legacy adapter claimed a complete runtime lookup outside its window")
	}
	for _, status := range []model.RunStatus{model.RunStatusFailed, model.RunStatusNeedsAttention} {
		t.Run(string(status), func(t *testing.T) {
			put(&model.AppDeploy{ID: string(status), AppID: "app", Kind: model.AppDeployKindDeploy, Status: status, CreatedAt: now.Add(200 * time.Second)})
			got, err := store.LatestRuntimeAttempts(ctx, h, []string{"app"})
			if err != nil || got["app"].Status != status {
				t.Fatalf("uncertain/failed runtime was skipped: %+v %v", got, err)
			}
		})
	}
}

func TestRuntimeHistoryLegacyOnlySkipsTypedRejection(t *testing.T) {
	ctx := context.Background()
	h := memory.New().AppDeploys
	now := time.Now().UTC()
	for _, d := range []*model.AppDeploy{
		{ID: "unknown", AppID: "app", Kind: model.AppDeployKind("future-kind"), Status: model.RunStatusFailed, CreatedAt: now},
		{ID: "rejected", AppID: "app", Kind: model.AppDeployKindRejected, Status: model.RunStatusFailed, CreatedAt: now.Add(time.Second)},
		{ID: "only-rejected", AppID: "other", Kind: model.AppDeployKindRejected, Status: model.RunStatusFailed, CreatedAt: now},
	} {
		if err := h.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.LatestRuntimeAttempts(ctx, legacyHistory{h}, []string{"app", "other"})
	if err != nil || got["app"].ID != "unknown" || got["other"] != nil {
		t.Fatalf("typed filtering invented a prior successful deployment: %+v %v", got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.LatestRuntimeAttempts(cancelled, h, []string{"app"}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
