package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

// Literal kind intentionally compiles against the frozen pre-repair source so
// the missing read boundary is demonstrated before the production enum exists.
// These are owned stores and local CLI fixtures, not Docker/cloud acceptance.
func addRuntimeRejectedAttempt(t *testing.T, runtime *RuntimeService, app *model.App, id string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	p := &model.Pipeline{ID: "rejected-graph-" + id, Stages: []model.Stage{}}
	if err := runtime.Store.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Store.AppDeploys.Create(ctx, &model.AppDeploy{ID: id, AppID: app.ID, RunID: "rejected-run-" + id, PipelineID: p.ID, Status: model.RunStatusFailed, Kind: model.AppDeployKind("rejected"), CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRejectedHistoryUsesPreviousRuntimeBeyondHistoryPage(t *testing.T) {
	runtime, app, _, _, history := runtimeSessionFixture(t, "run")
	// Restore the concrete owned store, exercising its optional bulk runtime
	// projection rather than the compatibility wrapper's bounded history page.
	runtime.Store.AppDeploys = history.AppDeployStore
	base := time.Now().UTC().Add(time.Minute)
	for i := range 101 {
		addRuntimeRejectedAttempt(t, runtime, app, fmt.Sprintf("reject-%03d", i), base.Add(time.Duration(i)*time.Second))
	}
	latest, err := runtime.Store.AppDeploys.ListByApp(context.Background(), app.ID, 1)
	if err != nil || len(latest) != 1 || latest[0].ID != "reject-100" || latest[0].Status != model.RunStatusFailed {
		t.Fatal("latest failed attempt disappeared from history")
	}
	observed, err := runtime.observe(context.Background(), app)
	if err != nil || observed.deployment.ID != "deployment" || observed.deployment.RunID != "run" || len(observed.stages) != 1 || observed.stages[0].Config.RuntimeName != "original-web-1" {
		t.Fatal("rejected attempts hid or replaced the recorded runtime", err)
	}
	runtime.DockerBin = runtimeOutput(t, "running|image|healthy|run")
	status, err := runtime.Status(context.Background(), app, "web")
	if err != nil || status.Ref != "original-web-1" || !status.Healthy {
		t.Fatal("runtime status followed a rejected attempt", err)
	}
	if health, _, _ := runtime.Probe(context.Background(), app); health != model.AppHealthHealthy {
		t.Fatal("rejected attempt replaced observed runtime health")
	}
}

func TestRuntimeRejectedHistoryLogsRetainDeploymentIdentityAndGeneration(t *testing.T) {
	runtime, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
	addRuntimeRejectedAttempt(t, runtime, app, "latest-reject", time.Now().UTC().Add(time.Minute))
	// Current App edits must not substitute a rejected attempt's environment or
	// a future configuration for the still-running deployment's redaction pins.
	app.EnvironmentID = "future-environment"
	app.DeployTarget = model.DeployTarget{Kind: model.DeployTargetKubernetes}
	resolved, err := runtime.LogEnvironment(context.Background(), app, jobs, resolver)
	if err != nil || resolved.Values["TOKEN"] != "old-secret-value" {
		t.Fatal("log generation followed rejected/current configuration", err)
	}
	var output bytes.Buffer
	if err := runtime.StreamDeploymentLogs(context.Background(), app, "web", jobs, resolver, &output); err != nil {
		t.Fatal("rejected attempt blocked logs from recorded deployment", err)
	}
	if strings.Contains(output.String(), "old-secret-value") || !strings.Contains(output.String(), "[REDACTED]") || !strings.Contains(output.String(), strings.Repeat("a", 64)) || strings.Contains(output.String(), "original-web-1") {
		t.Fatal("logs lost redaction or followed a mutable workload name")
	}
	stored, err := resolver.Environments.Get(context.Background(), "original")
	if err != nil {
		t.Fatal(err)
	}
	stored.PlainVars = map[string]string{"ROTATED": "true"}
	if err := resolver.Environments.Update(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runtime.StreamDeploymentLogs(context.Background(), app, "web", jobs, resolver, &output); err == nil || output.Len() != 0 {
		t.Fatal("skipping rejected attempts bypassed historical-generation fence")
	}
}

func TestRuntimeRejectedHistoryDoesNotSkipFailedOrUncertainRuntime(t *testing.T) {
	for _, state := range []model.RunStatus{model.RunStatusFailed, model.RunStatusNeedsAttention} {
		t.Run(string(state), func(t *testing.T) {
			runtime, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
			ctx := context.Background()
			base := time.Now().UTC().Add(time.Minute)
			graph := &model.Pipeline{ID: "later-runtime", Stages: []model.Stage{{ID: "web", Type: model.StageTypeDeploy, Config: model.StageConfig{RuntimeName: "later-web-1", ComposeServiceName: "web", DeployRuntime: "compose"}}}}
			if err := runtime.Store.Pipelines.Create(ctx, graph); err != nil {
				t.Fatal(err)
			}
			if err := runtime.Store.AppDeploys.Create(ctx, &model.AppDeploy{ID: "later-attempt", AppID: app.ID, RunID: "later-run", PipelineID: graph.ID, Status: state, Kind: model.AppDeployKindDeploy, CreatedAt: base}); err != nil {
				t.Fatal(err)
			}
			addRuntimeRejectedAttempt(t, runtime, app, "rejected-after-runtime", base.Add(time.Second))
			observed, err := runtime.observe(ctx, app)
			if err != nil || observed.deployment.ID != "later-attempt" || observed.deployment.Status != state || observed.deployment.RunID != "later-run" {
				t.Fatal("failed/uncertain runtime skipped in favor of an older success", err)
			}
			runtime.DockerBin = runtimeOutput(t, "running|image|healthy|later-run")
			want := model.AppHealthDegraded
			if state == model.RunStatusNeedsAttention {
				want = model.AppHealthUnknown
			}
			if health, _, _ := runtime.Probe(ctx, app); health != want {
				t.Fatalf("state %s: got health %s want %s", state, health, want)
			}
			var output bytes.Buffer
			if err := runtime.StreamDeploymentLogs(ctx, app, "web", jobs, resolver, &output); err == nil || output.Len() != 0 {
				t.Fatal("later failed/uncertain attempt borrowed older log authority")
			}
		})
	}
}

func TestRuntimeRejectedHistoryOnlyRejectionsHaveNoRuntimeOrLogGeneration(t *testing.T) {
	ctx := context.Background()
	st, jobs := memory.New(), jobqueue.NewMemory()
	app := &model.App{ID: "only-rejected", EnvironmentID: "unused-env"}
	env := &model.Environment{ID: "unused-env", Name: "Unused"}
	if err := st.Environments.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	env, _ = st.Environments.Get(ctx, env.ID)
	runtime := &RuntimeService{Store: st, SecretGenerationBound: true}
	addRuntimeRejectedAttempt(t, runtime, app, "only-rejected", time.Now().UTC())
	payload, _ := json.Marshal(AppDeployPlan{Version: 1, RunID: "rejected-run-only-rejected", App: app, EnvironmentVersion: env.Version})
	if _, err := jobs.Enqueue(ctx, JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:rejected-run-only-rejected"}); err != nil {
		t.Fatal(err)
	}
	resolver := &AppEnvResolver{Environments: st.Environments}
	if _, err := runtime.observe(ctx, app); err == nil {
		t.Fatal("only rejected attempts manufactured runtime inventory")
	}
	if health, _, _ := runtime.Probe(ctx, app); health != model.AppHealthUnknown {
		t.Fatal("only rejected attempts manufactured health")
	}
	if _, err := runtime.LogEnvironment(ctx, app, jobs, resolver); err == nil {
		t.Fatal("only rejected attempts manufactured historical log authority")
	}
	var output bytes.Buffer
	if err := runtime.StreamDeploymentLogs(ctx, app, "web", jobs, resolver, &output); err == nil || output.Len() != 0 {
		t.Fatal("only rejected attempts opened runtime logs")
	}
}
