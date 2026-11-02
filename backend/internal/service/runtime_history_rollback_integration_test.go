//go:build integration

package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/catalog"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/postgres"
)

// These stable read APIs deliberately compile against both the retained owner
// source and the newer candidate. The harness seeds once with the newer writer,
// then launches separate binaries against the same disposable PostgreSQL data.
// No worker, runtime inspection, credential or provider call occurs here.
func TestRuntimeHistoryRollbackPostgres(t *testing.T) {
	dsn := os.Getenv("COOKER_HISTORY_ROLLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned history rollback PostgreSQL fixture only")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Host != "cooker-history-rollback-pg:5432" || u.Path != "/cooker_history_rollback_test" || u.User == nil || u.User.Username() != "history_rollback_fixture" || u.RawQuery != "sslmode=disable" {
		t.Fatal("refusing a database outside the owned rollback fixture")
	}
	if _, present := u.User.Password(); present {
		t.Fatal("fixture must not carry a credential")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := postgres.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal("opening owned fixture store", err)
	}
	defer st.Close()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	jobs := jobqueue.NewPostgres(db)
	cases := []struct {
		name   string
		kind   model.AppDeployKind
		status model.RunStatus
		count  int
	}{
		{"success-beyond-page", model.AppDeployKindDeploy, model.RunStatusSuccess, 120},
		{"failed-runtime", model.AppDeployKindDeploy, model.RunStatusFailed, 1},
		{"uncertain-runtime", model.AppDeployKindDeploy, model.RunStatusNeedsAttention, 1},
		{"compose-runtime", model.AppDeployKindDeploy, model.RunStatusSuccess, 1},
		{"unknown-kind-runtime", model.AppDeployKind("future-runtime-kind"), model.RunStatusFailed, 1},
		{"only-rejected", "", "", 1},
	}
	switch os.Getenv("COOKER_HISTORY_ROLLBACK_TEST_PHASE") {
	case "seed":
		var existing int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM apps`).Scan(&existing); err != nil || existing != 0 {
			t.Fatal("seed requires an empty owned database")
		}
		base := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
		for _, tc := range cases {
			appID := "history-compat-" + tc.name
			env := &model.Environment{ID: appID + "-recorded-env", Name: appID + " recorded", PlainVars: map[string]string{"CONFIG": "recorded"}}
			if err := st.Environments.Create(ctx, env); err != nil {
				t.Fatal(err)
			}
			env, err = st.Environments.Get(ctx, env.ID)
			if err != nil {
				t.Fatal(err)
			}
			future := &model.Environment{ID: appID + "-future-env", Name: appID + " future", PlainVars: map[string]string{"CONFIG": "future"}}
			if err := st.Environments.Create(ctx, future); err != nil {
				t.Fatal(err)
			}
			future, err = st.Environments.Get(ctx, future.ID)
			if err != nil {
				t.Fatal(err)
			}
			app := &model.App{ID: appID, Name: tc.name, GitHubRepo: "owned/history-compat", Branch: "main", EnvironmentID: future.ID, DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes}}
			if err := st.Apps.Create(ctx, app); err != nil {
				t.Fatal(err)
			}
			put := func(d *model.AppDeploy, at time.Time, runtime bool) {
				t.Helper()
				stages := []model.Stage{}
				stageRuns := []model.StageRun{}
				if runtime {
					stages = []model.Stage{{ID: "web", Name: "Website", Type: model.StageTypeDeploy, EnvironmentID: env.ID, Config: model.StageConfig{ComposeServiceName: "web", RuntimeName: appID + "-recorded-web", DeployRuntime: "docker"}}}
					stageRuns = []model.StageRun{{StageID: "web", Status: d.Status}}
				}
				if err := st.Pipelines.Create(ctx, &model.Pipeline{ID: d.PipelineID, Name: "Recorded history graph", Stages: stages}); err != nil {
					t.Fatal(err)
				}
				if err := st.Runs.Create(ctx, &model.PipelineRun{ID: d.RunID, PipelineID: d.PipelineID, Status: d.Status, CreatedAt: at, StageRuns: stageRuns}); err != nil {
					t.Fatal(err)
				}
				if err := st.AppDeploys.Create(ctx, d); err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(ctx, `UPDATE app_deploys SET created_at=$1 WHERE id=$2`, at, d.ID); err != nil {
					t.Fatal(err)
				}
				accepted := *app
				accepted.EnvironmentID = env.ID
				version := env.Version
				if !runtime {
					accepted.EnvironmentID, version = future.ID, future.Version
				}
				payload, err := json.Marshal(AppDeployPlan{Version: 1, RunID: d.RunID, App: &accepted, Commit: strings.Repeat("a", 40), EnvironmentVersion: version, CreatedAt: at})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := jobs.Enqueue(ctx, JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:" + d.RunID, MaxAttempts: 1}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.kind != "" {
				d := &model.AppDeploy{ID: appID + "-runtime", AppID: appID, RunID: appID + "-runtime-run", PipelineID: appID + "-runtime-graph", Kind: tc.kind, Status: tc.status}
				if tc.name != "compose-runtime" {
					d.ImageRef, d.Digest = "owned.invalid/fixture@sha256:"+strings.Repeat("b", 64), "sha256:"+strings.Repeat("b", 64)
				}
				put(d, base, true)
			}
			for i := 1; i <= tc.count; i++ {
				id := fmt.Sprintf("%s-rejected-%03d", appID, i)
				put(&model.AppDeploy{ID: id, AppID: appID, RunID: id + "-run", PipelineID: id + "-graph", Kind: model.AppDeployKind("rejected"), Status: model.RunStatusFailed}, base.Add(time.Duration(i)*time.Second), false)
			}
		}
		t.Log("seeded six persisted history scenarios using the candidate writer")
	case "read":
		before := historyRollbackDataHash(t, ctx, db)
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				appID := "history-compat-" + tc.name
				app, err := st.Apps.Get(ctx, appID)
				if err != nil {
					t.Fatal(err)
				}
				last := fmt.Sprintf("%s-rejected-%03d", appID, tc.count)
				rows, err := st.AppDeploys.ListByApp(ctx, appID, 1)
				if err != nil || len(rows) != 1 || rows[0].ID != last || rows[0].Kind != model.AppDeployKind("rejected") || rows[0].Status != model.RunStatusFailed || rows[0].ImageRef != "" || rows[0].Digest != "" {
					t.Fatal("reader lost or reinterpreted failed rejection history", err)
				}
				inventory := &catalog.Service{Store: st}
				detail, err := inventory.App(ctx, appID, false)
				if err != nil || detail.App.LatestDeployment == nil || detail.App.LatestDeployment.ID != last || detail.App.PipelineID != last+"-graph" || len(detail.Deployments) == 0 || detail.Deployments[0].ID != last {
					t.Fatal("catalog hid the latest rejected attempt", err)
				}
				services := inventory.Services(ctx, catalog.Query{AppID: appID}, false)
				runtime := &RuntimeService{Store: st, SecretGenerationBound: true}
				observed, observeErr := runtime.observe(ctx, app)
				resolver := &AppEnvResolver{Environments: st.Environments}
				resolved, logErr := runtime.LogEnvironment(ctx, app, jobs, resolver)
				if tc.kind == "" {
					if len(detail.Services) != 0 || services.Total != 0 || observeErr == nil || logErr == nil {
						t.Fatal("only-rejected App gained runtime or log authority")
					}
					return
				}
				if len(detail.Services) != 1 || services.Total != 1 || detail.Services[0].DeploymentID != appID+"-runtime" || services.Items[0].DeploymentID != appID+"-runtime" || detail.Services[0].Status != string(tc.status) || detail.Services[0].Health != "unknown" {
					t.Errorf("catalog did not preserve recorded runtime separately from latest rejection: detail=%d global=%d", len(detail.Services), services.Total)
				}
				if observeErr != nil || observed == nil || observed.deployment.ID != appID+"-runtime" || observed.deployment.Kind != tc.kind || observed.deployment.Status != tc.status || len(observed.stages) != 1 || observed.stages[0].Config.RuntimeName != appID+"-recorded-web" {
					t.Error("runtime observation followed rejection or skipped a failed/uncertain/unknown runtime", observeErr)
				}
				if logErr != nil || resolved == nil || resolved.Values["CONFIG"] != "recorded" {
					t.Error("log configuration followed current App or rejected attempt instead of recorded generation", logErr)
				}
			})
		}
		if after := historyRollbackDataHash(t, ctx, db); before != after {
			t.Fatal("read campaign mutated persisted history, jobs or schema")
		}
		t.Log("all durable rows and migration records unchanged across reads")
	default:
		t.Fatal("owned harness must specify seed or read phase")
	}
}

func historyRollbackDataHash(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	h := sha256.New()
	for _, table := range []string{"apps", "app_deploys", "pipelines", "pipeline_runs", "environments", "jobs", "schema_migrations"} {
		key := "id"
		if table == "schema_migrations" {
			key = "version"
		}
		var data sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(t) ORDER BY `+key+`)::text FROM `+table+` t`).Scan(&data); err != nil {
			t.Fatal("fingerprinting fixture persistence", err)
		}
		fmt.Fprintln(h, table, data.String)
	}
	return hex.EncodeToString(h.Sum(nil))
}
