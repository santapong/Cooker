package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestAppQueueSnapshotsCommitAndConfigurationWithoutWebhookSecret(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	jobs := jobqueue.NewMemory()
	sha := strings.Repeat("a", 40)
	app := &model.App{ID: "app", Name: "demo", GitHubRepo: "acme/demo", Branch: "main", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Args: map[string]string{"MODE": "original"}}, WebhookSecret: []byte("do-not-persist")}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: &AppDeployer{}, ResolveCommit: func(context.Context, int64, string, string) (string, error) { return sha, nil }}
	r, err := q.Enqueue(ctx, app, "", &model.PipelineRun{StartedByUserSub: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	app.BuildPlan.Args["MODE"] = "changed"
	j, err := jobs.Get(ctx, r.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(j.Payload), "do-not-persist") {
		t.Fatal("webhook secret persisted")
	}
	var p AppDeployPlan
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Commit != sha || p.App.BuildPlan.Args["MODE"] != "original" || p.Actor.StartedByUserSub != "operator" {
		t.Fatalf("snapshot changed: %+v", p)
	}
	if len(r.PlanDigest) != 64 || j.MaxAttempts != 1 || j.ConcurrencyKey != "app:app" {
		t.Fatalf("unsafe receipt/job: %+v %+v", r, j)
	}
	// Materialization is idempotent and cannot overwrite a completed run.
	run, _ := st.Runs.Get(ctx, r.RunID)
	run.Status = model.RunStatusSuccess
	if err := st.Runs.Update(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := q.ensureRun(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Runs.Get(ctx, r.RunID)
	if got.Status != model.RunStatusSuccess {
		t.Fatal("run overwritten during retry")
	}
	if err := q.Handle(ctx, j); err != nil {
		t.Fatal(err)
	}
}

func TestAppQueueRejectsEnvironmentDriftBeforeDeployment(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	jobs := jobqueue.NewMemory()
	app := &model.App{ID: "app", Name: "demo", GitHubRepo: "acme/demo", Branch: "main", EnvironmentID: "env", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	env := &model.Environment{ID: "env", Name: "dev", PlainVars: map[string]string{"NAME": "one"}}
	if err := st.Environments.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: &AppDeployer{}}
	r, err := q.Enqueue(ctx, app, strings.Repeat("b", 40), nil)
	if err != nil {
		t.Fatal(err)
	}
	env.PlainVars["NAME"] = "two"
	if err := st.Environments.Update(ctx, env); err != nil {
		t.Fatal(err)
	}
	j := claimAppPreflightTest(t, q)
	if err := q.Handle(appPreflightTestContext(q, j), j); err != nil {
		t.Fatal(err)
	}
	run, _ := st.Runs.Get(ctx, r.RunID)
	if run.Status != model.RunStatusFailed || !strings.Contains(run.Error, "environment changed") {
		t.Fatalf("did not fail closed: %+v", run)
	}
}

func TestQueueSnapshotSurvivesPipelineEdit(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	jobs := jobqueue.NewMemory()
	p, run := simpleBuildPushPipeline("pipeline", "run")
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	e := NewJobQueueEnqueuer(jobs, st.Pipelines)
	if err := e.EnqueueRun(ctx, p.ID, run.ID); err != nil {
		t.Fatal(err)
	}
	p.Stages[0].Config.Tags = []string{"changed"}
	if err := st.Pipelines.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	j, _ := jobs.Dequeue(ctx, "worker", nil)
	var saved runJobPayload
	if err := json.Unmarshal(j.Payload, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Pipeline == nil || saved.Pipeline.Stages[0].Config.Tags[0] != "latest" {
		t.Fatal("queued definition drifted")
	}
	if err := NewJobQueueRunner(st, NewExecutor(), nil).Handle(ctx, j); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Runs.Get(ctx, run.ID)
	if got.Status != model.RunStatusSuccess {
		t.Fatal(got.Status)
	}
	if err := st.Runs.Create(ctx, got); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate run must conflict: %v", err)
	}
}

func TestAppQueueIntentAdmissionIsStableAcrossRedelivery(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	jobs := jobqueue.NewMemory()
	app := &model.App{ID: "shop", Name: "shop", GitHubRepo: "acme/shop", Branch: "main", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: &AppDeployer{}}
	sha := strings.Repeat("c", 40)
	one, err := q.EnqueueIntent(ctx, app, sha, nil, "verified-intent")
	if err != nil {
		t.Fatal(err)
	}
	app.Name = "changed after admission"
	two, err := q.EnqueueIntent(ctx, app, sha, nil, "verified-intent")
	if err != nil || *one != *two {
		t.Fatal("receipt changed on retry", one, two, err)
	}
	if _, err := q.EnqueueIntent(ctx, app, strings.Repeat("d", 40), nil, "verified-intent"); err == nil {
		t.Fatal("same intent accepted another revision")
	}
	counts, _ := jobs.Stats(ctx)
	if counts[jobqueue.StatusPending] != 1 {
		t.Fatal("duplicate job", counts)
	}
}
