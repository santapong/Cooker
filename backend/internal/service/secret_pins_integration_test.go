//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// Actual disposable PostgreSQL and a metadata/value fixture. Fresh store/worker
// objects are checked here; this is not a cloud call or process-kill campaign.
func TestSecretPinsPostgresPersistenceAndFreshWorker(t *testing.T) {
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned disposable PostgreSQL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "cooker-roadmap-notifications-pg-20261002" || u.Path != "/cooker_notifications_test" {
		t.Fatal("refusing non-owned secret-version fixture")
	}
	ctx := context.Background()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "secret_pins_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	st, err := storepg.NewStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	_, _, manager, env := immutableSecretFixture(t)
	oldID := env.ID
	env.ID = uuid.NewString()
	env.Version = 0
	manager.data[env.ID] = manager.data[oldID]
	delete(manager.data, oldID)
	if err := st.Environments.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	resolver := &AppEnvResolver{Environments: st.Environments, Secrets: manager}
	app := &model.App{ID: uuid.NewString(), Name: "Owned secret fixture", GitHubRepo: "owned/fixture", Branch: "main", EnvironmentID: env.ID, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewPostgres(db)
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: &AppDeployer{EnvResolver: resolver}, RequireSecretPins: true}
	receipt, err := q.EnqueueIntent(ctx, app, strings.Repeat("a", 40), nil, "owned-fixture-intent")
	if err != nil {
		t.Fatal(err)
	}
	p, run := singleStagePipeline(model.StageTypeTest, model.StageConfig{Image: "owned-fixture", SecretRefs: []string{"TOKEN"}})
	p.ID = uuid.NewString()
	p.Name = "Owned native fixture"
	p.Stages[0].EnvironmentID = env.ID
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	run.ID = uuid.NewString()
	run.PipelineID = p.ID
	run.PipelineVersion = p.Version
	exec := NewExecutor(WithEnvironmentResolver(resolver))
	enq := NewJobQueueEnqueuer(jobs, st.Pipelines)
	enq.PrepareSecrets = exec.PreparePipelineSecrets
	if err := enq.EnqueuePlannedRun(ctx, p, run); err != nil {
		t.Fatal(err)
	}
	st.Close()
	db.Close()
	st, err = storepg.NewStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err = sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	jobs = jobqueue.NewPostgres(db)
	resolver = &AppEnvResolver{Environments: st.Environments, Secrets: manager}
	manager.latest = "2"
	manager.data[env.ID]["TOKEN"] = []byte("canary-rotated-secret")
	q = &AppDeployQueue{Jobs: jobs, Store: st, Deployer: &AppDeployer{EnvResolver: resolver}, RequireSecretPins: true}
	retry, err := q.EnqueueIntent(ctx, app, strings.Repeat("a", 40), nil, "owned-fixture-intent")
	if err != nil || retry.PlanDigest != receipt.PlanDigest {
		t.Fatal("PostgreSQL retry changed accepted secret plan")
	}
	got, err := deploymentLogEnvironment(ctx, app.ID, &model.AppDeploy{AppID: app.ID, RunID: receipt.RunID}, jobs, resolver, true)
	if err != nil || got.Values["TOKEN"] != "canary-original-secret" {
		t.Fatal("persisted App pins did not restore historical context")
	}
	job, err := jobs.Get(ctx, "run:"+run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(job.Payload), "canary-") {
		t.Fatal("plaintext entered persisted native plan")
	}
	runner := &fakeStageRunner{}
	if err := NewJobQueueRunner(st, NewExecutor(WithEnvironmentResolver(resolver), WithStageRunner(runner)), nil).Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
	actual, err := st.Runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(actual)
	if actual.Status != model.RunStatusSuccess || runner.callCount() != 1 || runner.calls[0].Env["TOKEN"] != "canary-original-secret" || strings.Contains(string(data), "canary-") {
		t.Fatal("fresh native worker lost pins or leaked values")
	}
}
