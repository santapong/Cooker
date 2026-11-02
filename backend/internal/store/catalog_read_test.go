package store_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
	"github.com/santapong/cooker/internal/store/postgres"
)

func TestCatalogReaderContracts(t *testing.T) {
	t.Run("memory", func(t *testing.T) { catalogReaderContract(t, memory.New()) })
	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
		if dsn == "" {
			t.Skip("COOKER_INTEGRATION_DATABASE_URL required")
		}
		st, err := postgres.NewStore(context.Background(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = st.Close() }()
		catalogReaderContract(t, st)
	})
}
func catalogReaderContract(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	prefix := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Millisecond)
	canary := "catalog-private-canary"
	pipes := st.Pipelines.(store.CatalogPipelineReader)
	deploys := st.AppDeploys.(store.CatalogAppDeployReader)
	for _, suffix := range []string{"full", "empty"} {
		p := &model.Pipeline{ID: prefix + suffix, Name: "Catalog " + suffix, CreatedAt: now, UpdatedAt: now, Variables: map[string]string{"PRIVATE": canary}}
		p.SetDefaults()
		if suffix == "full" {
			p.Stages = []model.Stage{{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{BuildArgs: map[string]string{"private": canary}}}, {ID: "deploy", Type: model.StageTypeDeploy}}
		}
		if err := st.Pipelines.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Pipelines.Delete(ctx, p.ID) })
	}
	for _, suffix := range []string{"old", "a", "z"} {
		created := now
		if suffix == "old" {
			created = now.Add(-time.Hour)
		}
		r := &model.PipelineRun{ID: prefix + suffix, PipelineID: prefix + "full", Status: model.RunStatusSuccess, CreatedAt: created, StartedAt: &created, FinishedAt: &created, Variables: map[string]string{"private": canary}, StageRuns: []model.StageRun{{StageID: "build", Logs: strings.Repeat(canary, 20000), Outputs: map[string]string{"private": canary}}}, EnvironmentStatuses: []model.EnvironmentStatus{{EnvironmentID: canary}}, StartedByEmail: canary, StartedByTokenHash: canary, Error: canary}
		if err := st.Runs.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := pipes.ListCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var full, empty *store.CatalogPipelineSummary
	for i := range rows {
		switch rows[i].ID {
		case prefix + "full":
			full = &rows[i]
		case prefix + "empty":
			empty = &rows[i]
		}
	}
	if full == nil || full.StageCount != 2 || full.LatestRun == nil || full.LatestRun.ID != prefix+"z" || empty == nil || empty.StageCount != 0 || empty.LatestRun != nil {
		t.Fatal("latest run/empty pipeline contract lost")
	}
	b, _ := json.Marshal(full)
	if strings.Contains(string(b), canary) || len(b) > 2048 || full.LatestRun.StageRuns != nil || full.LatestRun.Variables != nil || full.LatestRun.EnvironmentStatuses != nil {
		t.Fatal("catalog projection included private/full payload")
	}
	*full.LatestRun.StartedAt = time.Time{}
	again, err := pipes.ListCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range again {
		if p.ID == full.ID && p.LatestRun.StartedAt.IsZero() {
			t.Fatal("summary aliased stored lifecycle timestamp")
		}
	}
	for _, suffix := range []string{"one", "two", "unselected"} {
		app := &model.App{ID: prefix + suffix, Name: prefix + suffix, GitHubRepo: "fixture/" + prefix + suffix, CreatedAt: now, UpdatedAt: now}
		if err := st.Apps.Create(ctx, app); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Apps.Delete(ctx, app.ID) })
		for _, version := range []string{"old", "new"} {
			d := &model.AppDeploy{ID: prefix + suffix + version, AppID: app.ID, PipelineID: prefix + "full", RunID: prefix + "z", Status: model.RunStatusSuccess}
			if err := st.AppDeploys.Create(ctx, d); err != nil {
				t.Fatal(err)
			}
		}
	}
	latest, err := deploys.LatestByApps(ctx, []string{prefix + "one", prefix + "two", prefix + "one", prefix + "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 || latest[prefix+"one"].ID != prefix+"onenew" || latest[prefix+"two"].ID != prefix+"twonew" {
		t.Fatalf("batch scope/latest/dedupe broken: %+v", latest)
	}
	latest[prefix+"one"].Status = model.RunStatusFailed
	next, err := deploys.LatestByApps(ctx, []string{prefix + "one"})
	if err != nil || next[prefix+"one"].Status != model.RunStatusSuccess {
		t.Fatal("batch projection mutated persisted state")
	}
	if empty, err := deploys.LatestByApps(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatal("empty selection returned resources")
	}
}
