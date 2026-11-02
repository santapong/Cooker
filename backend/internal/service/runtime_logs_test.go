package service

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
	"gopkg.in/yaml.v3"
)

type runtimeHistoryCounter struct {
	store.AppDeployStore
	calls int
}

func (s *runtimeHistoryCounter) ListByApp(ctx context.Context, id string, limit int) ([]*model.AppDeploy, error) {
	s.calls++
	return s.AppDeployStore.ListByApp(ctx, id, limit)
}

type runtimeEnvHook struct {
	store.EnvironmentStore
	once func()
}

func (s *runtimeEnvHook) Get(ctx context.Context, id string) (*model.Environment, error) {
	if s.once != nil {
		hook := s.once
		s.once = nil
		hook()
	}
	return s.EnvironmentStore.Get(ctx, id)
}

// These are hermetic CLI/API fixtures, not live Docker/Kubernetes evidence.
func runtimeSessionFixture(t *testing.T, label string) (*RuntimeService, *model.App, *jobqueue.Memory, *AppEnvResolver, *runtimeHistoryCounter) {
	t.Helper()
	ctx := context.Background()
	st, jobs := memory.New(), jobqueue.NewMemory()
	app := &model.App{ID: "app", EnvironmentID: "original"}
	env := &model.Environment{ID: "original", Name: "Original"}
	if err := st.Environments.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	env, _ = st.Environments.Get(ctx, env.ID)
	p := &model.Pipeline{ID: "pipeline", Stages: []model.Stage{{ID: "deploy-web", Type: model.StageTypeDeploy, Config: model.StageConfig{RuntimeName: "original-web-1", ComposeServiceName: "web", DeployRuntime: "compose"}}}}
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.AppDeploys.Create(ctx, &model.AppDeploy{ID: "deployment", AppID: app.ID, RunID: "run", PipelineID: p.ID, Status: model.RunStatusSuccess}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(AppDeployPlan{Version: 1, RunID: "run", App: app, EnvironmentVersion: env.Version})
	if _, err := jobs.Enqueue(ctx, JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:run"}); err != nil {
		t.Fatal(err)
	}
	history := &runtimeHistoryCounter{AppDeployStore: st.AppDeploys}
	st.AppDeploys = history
	bin := filepath.Join(t.TempDir(), "docker-fixture")
	script := "#!/bin/sh\ncase \"$1\" in\ninspect) printf '%s\\n' '" + strings.Repeat("a", 64) + "|" + label + "';;\nlogs) printf '%s\\n' \"$*\"; printf '%s\\n' 'old-secret-value';;\n*) exit 1;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	resolver := &AppEnvResolver{Environments: st.Environments, Secrets: &fakeSecrets{data: map[string]map[string][]byte{"original": {"TOKEN": []byte("old-secret-value")}}}}
	return &RuntimeService{Store: st, DockerBin: bin, SecretGenerationBound: true}, app, jobs, resolver, history
}

func TestRuntimeLogSessionRequiresBoundSecretGenerationOnlyForSecrets(t *testing.T) {
	r, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
	r.SecretGenerationBound = false
	var out bytes.Buffer
	if err := r.StreamDeploymentLogs(context.Background(), app, "web", jobs, resolver, &out); err == nil || !strings.Contains(err.Error(), "backend generation") || out.Len() != 0 {
		t.Fatal("unversioned secrets accepted")
	}
	resolver.Secrets = &fakeSecrets{data: map[string]map[string][]byte{}}
	if err := r.StreamDeploymentLogs(context.Background(), app, "web", jobs, resolver, &out); err == nil {
		t.Fatal("deleted external secrets mistaken for historical plain-only config")
	}
	job, _ := jobs.Get(context.Background(), "app-run:run")
	var plan AppDeployPlan
	if err := json.Unmarshal(job.Payload, &plan); err != nil {
		t.Fatal(err)
	}
	plan.App.BuildPlan = &model.BuildPlan{Kind: model.BuildPlanCompose, EnvironmentScope: ServiceEnvironmentScope, ServiceEnvironment: map[string][]string{}}
	payload, _ := json.Marshal(plan)
	jobs = jobqueue.NewMemory()
	if _, err := jobs.Enqueue(context.Background(), JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:run"}); err != nil {
		t.Fatal(err)
	}
	if err := r.StreamDeploymentLogs(context.Background(), app, "web", jobs, resolver, &out); err != nil {
		t.Fatalf("plain environment blocked: %v", err)
	}
}

func TestRuntimeLogSessionPinsHistoryAndContainerBeforeNewDeployment(t *testing.T) {
	r, app, jobs, resolver, history := runtimeSessionFixture(t, "run")
	resolver.Environments = &runtimeEnvHook{EnvironmentStore: resolver.Environments, once: func() {
		// A newer deployment completes after inspection while secrets resolve.
		if err := r.Store.AppDeploys.Create(context.Background(), &model.AppDeploy{ID: "new", AppID: app.ID, RunID: "new-run", PipelineID: "new-graph", Status: model.RunStatusSuccess, CreatedAt: time.Now().Add(time.Second)}); err != nil {
			t.Fatal(err)
		}
	}}
	app.EnvironmentID = "future-environment"
	app.DeployTarget = model.DeployTarget{Kind: model.DeployTargetKubernetes, ExternalServices: []model.ExternalServiceBinding{{Service: "web", Resource: "future-external"}}}
	var out bytes.Buffer
	if err := r.StreamDeploymentLogs(context.Background(), app, "web", jobs, resolver, &out); err != nil {
		t.Fatal(err)
	}
	if history.calls != 1 || strings.Contains(out.String(), "old-secret-value") || !strings.Contains(out.String(), "[REDACTED]") {
		t.Fatalf("history=%d output=%q", history.calls, out.String())
	}
	if !strings.Contains(out.String(), strings.Repeat("a", 64)) || strings.Contains(out.String(), "original-web-1") {
		t.Fatal("log stream followed mutable service name")
	}
}

func TestRuntimeLogSessionRejectsReplacedOrLegacyContainer(t *testing.T) {
	for _, label := range []string{"new-run", "<no value>", ""} {
		t.Run(label, func(t *testing.T) {
			r, app, jobs, resolver, _ := runtimeSessionFixture(t, label)
			var out bytes.Buffer
			if err := r.StreamDeploymentLogs(context.Background(), app, "web", jobs, resolver, &out); err == nil || out.Len() != 0 {
				t.Fatal("unverified container emitted logs")
			}
		})
	}
}

func TestRuntimeLogSessionFailsClosedForRotationAndUnknownService(t *testing.T) {
	r, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
	var out bytes.Buffer
	if err := r.StreamDeploymentLogs(context.Background(), app, "other-app", jobs, resolver, &out); err == nil {
		t.Fatal("unknown service accepted")
	}
	env, _ := resolver.Environments.Get(context.Background(), "original")
	env.PlainVars = map[string]string{"ROTATED": "yes"}
	if err := resolver.Environments.Update(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if err := r.StreamDeploymentLogs(context.Background(), app, "web", jobs, resolver, &out); err == nil || out.Len() != 0 {
		t.Fatal("rotated generation emitted historical logs")
	}
}

func TestRuntimeHealthUsesOneRecordedSnapshotAndRejectsNewRun(t *testing.T) {
	r, app, _, _, history := runtimeSessionFixture(t, "run")
	r.DockerBin = runtimeOutput(t, "running|image|healthy|run")
	app.DeployTarget.ExternalServices = []model.ExternalServiceBinding{{Service: "web", Resource: "next-config"}}
	if state, _, _ := r.Probe(context.Background(), app); state != model.AppHealthHealthy || history.calls != 1 {
		t.Fatalf("mixed or edited health snapshot: %s, history reads=%d", state, history.calls)
	}
	r.DockerBin = runtimeOutput(t, "running|image|healthy|new-run")
	if state, _, _ := r.Probe(context.Background(), app); state != model.AppHealthUnknown {
		t.Fatal("different deployment reported healthy")
	}
	p, _ := r.Store.Pipelines.Get(context.Background(), "pipeline")
	p.Stages[0].Config.DeployRuntime = "kubernetes"
	if err := r.Store.Pipelines.Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		output string
		want   model.AppHealth
	}{{"2/2/2/7/7 image run", model.AppHealthHealthy}, {"2/2/2/7/7 image different-run", model.AppHealthUnknown}, {"2/2/2/7/6 image run", model.AppHealthDegraded}} {
		r.KubectlBin = runtimeOutput(t, tc.output)
		if state, _, _ := r.Probe(context.Background(), app); state != tc.want {
			t.Fatalf("%s: got %s want %s", tc.output, state, tc.want)
		}
	}
}

func TestRuntimeLogSessionKubernetesRequiresFutureImmutableAdapter(t *testing.T) {
	r, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
	p, _ := r.Store.Pipelines.Get(context.Background(), "pipeline")
	p.Stages[0].Config.DeployRuntime = "kubernetes"
	if err := r.Store.Pipelines.Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := r.StreamDeploymentLogs(context.Background(), app, "web", jobs, resolver, &out)
	if err == nil || !strings.Contains(err.Error(), "Docker deployments only") || out.Len() != 0 {
		t.Fatalf("unsupported logs: %v", err)
	}
}

func TestRuntimeLabelsOverrideRepositoryLabelWithoutChangingSelectors(t *testing.T) {
	manifest := []byte("kind: Deployment\nmetadata:\n  name: demo\nspec:\n  selector:\n    matchLabels: {app: demo}\n  template:\n    metadata:\n      labels: {app: demo, io.cooker.run-id: forged}\n    spec:\n      containers: [{name: web, image: demo}]\n---\nkind: Service\nmetadata: {name: demo}\nspec:\n  selector: {app: demo}\n")
	data, err := stampRuntimeManifest(manifest, "trusted-run")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	spec := doc["spec"].(map[string]any)
	labels := spec["template"].(map[string]any)["metadata"].(map[string]any)["labels"].(map[string]any)
	if labels[RuntimeRunLabel] != "trusted-run" || labels["app"] != "demo" || len(spec["selector"].(map[string]any)["matchLabels"].(map[string]any)) != 1 {
		t.Fatalf("invalid template/selector: %v", doc)
	}
	file, cleanup, err := composeImageOverride("web", "image:legacy", "trusted-run")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err = os.ReadFile(file)
	if err != nil || !bytes.Contains(data, []byte(RuntimeRunLabel)) || !bytes.Contains(data, []byte("trusted-run")) {
		t.Fatalf("missing Compose run identity: %v", err)
	}
}

type runtimeDeployCapture func(context.Context, deployer.Request) (deployer.Result, error)

func (f runtimeDeployCapture) Deploy(ctx context.Context, req deployer.Request) (deployer.Result, error) {
	return f(ctx, req)
}

func TestRuntimeExecutorStampsAuthoritativeRunIdentity(t *testing.T) {
	for _, runtime := range []string{"docker", "compose", "kubernetes"} {
		t.Run(runtime, func(t *testing.T) {
			called := false
			capture := runtimeDeployCapture(func(_ context.Context, req deployer.Request) (deployer.Result, error) {
				called = true
				switch runtime {
				case "docker":
					if req.Labels[RuntimeRunLabel] != "executor-run" {
						t.Fatal("Docker run identity missing")
					}
				case "compose":
					data, err := os.ReadFile(req.ComposeOverrideFile)
					if err != nil || !bytes.Contains(data, []byte("executor-run")) || !bytes.Contains(data, []byte(RuntimeRunLabel)) {
						t.Fatalf("Compose run identity missing: %v", err)
					}
				case "kubernetes":
					if !bytes.Contains(req.Manifest, []byte("io.cooker.run-id: executor-run")) {
						t.Fatal("Kubernetes run identity missing")
					}
				}
				return deployer.Result{}, nil
			})
			e := NewExecutor(WithDeployer(capture), WithDockerDeployer(capture), WithComposeDeployer(capture))
			stage := &model.Stage{ID: "deploy-web", Type: model.StageTypeDeploy, Config: model.StageConfig{DeployRuntime: runtime, RuntimeName: "web", ComposeServiceName: "web", ComposeProject: "app", Image: "redis:7", ManifestPath: "kind: Deployment\nspec:\n  template:\n    spec:\n      containers: [{name: web, image: redis:7}]\n"}}
			if err := e.executeDeploy(context.Background(), "executor-run", stage, &model.StageRun{StageID: stage.ID}); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("deployer not reached")
			}
		})
	}
}
