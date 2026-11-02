//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func TestRuntimeAttemptPostgresFiltersBeforeLatestSelection(t *testing.T) {
	_, dsn := registryAdmissionTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, id := range []string{"app", "only-rejected"} {
		if err := st.Apps.Create(ctx, &model.App{ID: id, Name: id, GitHubRepo: "owned/runtime-history", Branch: "main"}); err != nil {
			t.Fatal(err)
		}
	}
	put := func(id, app string, kind model.AppDeployKind, status model.RunStatus, order int) {
		t.Helper()
		if _, err := st.AppDeploys.(*AppDeployStore).db.ExecContext(ctx, `INSERT INTO app_deploys(id,app_id,run_id,kind,status,created_at) VALUES($1,$2,$1,$3,$4,$5)`, id, app, kind, status, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC).Add(time.Duration(order)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	put("runtime", "app", model.AppDeployKindDeploy, model.RunStatusSuccess, 0)
	for i := 1; i <= 120; i++ {
		put(fmt.Sprintf("rejected-%03d", i), "app", model.AppDeployKindRejected, model.RunStatusFailed, i)
	}
	put("only", "only-rejected", model.AppDeployKindRejected, model.RunStatusFailed, 0)
	h := st.AppDeploys.(*AppDeployStore)
	latest, err := h.LatestByApps(ctx, []string{"app"})
	if err != nil || latest["app"].Kind != model.AppDeployKindRejected {
		t.Fatalf("attempt summary filtered: %+v %v", latest, err)
	}
	got, err := h.LatestRuntimeAttemptByApps(ctx, []string{"app", "only-rejected", "absent"})
	if err != nil || got["app"].ID != "runtime" || len(got) != 1 {
		t.Fatalf("SQL latest-before-filter hid runtime: %+v %v", got, err)
	}
	for i, status := range []model.RunStatus{model.RunStatusFailed, model.RunStatusNeedsAttention} {
		put(string(status), "app", model.AppDeployKindDeploy, status, 200+i)
		got, err = h.LatestRuntimeAttemptByApps(ctx, []string{"app"})
		if err != nil || got["app"].Status != status {
			t.Fatalf("runtime outcome skipped: %+v %v", got, err)
		}
	}
	fresh, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	got, err = fresh.AppDeploys.(*AppDeployStore).LatestRuntimeAttemptByApps(ctx, []string{"app"})
	if err != nil || got["app"].Status != model.RunStatusNeedsAttention {
		t.Fatal("reopened reader changed selection", got, err)
	}
}
