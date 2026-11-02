package service

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

func runtimeOutput(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime-fixture")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+output+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRuntimeDockerHealthRequiresObservedCheck(t *testing.T) {
	for _, tc := range []struct {
		output  string
		healthy bool
		state   string
	}{
		{"running|redis@sha256:abc|healthy", true, "running"},
		{"running|redis@sha256:abc|unhealthy", false, "degraded"},
		{"running|redis@sha256:abc|starting", false, "degraded"},
		{"running|redis@sha256:abc|", false, "running"},
		{"exited|redis@sha256:abc|healthy", false, "exited"},
	} {
		r := &RuntimeService{DockerBin: runtimeOutput(t, tc.output)}
		s, err := r.Status(context.Background(), runtimeTestDockerApp(), "web")
		if err != nil || s.Healthy != tc.healthy || s.State != tc.state {
			t.Fatalf("%s: %+v %v", tc.output, s, err)
		}
	}
}

func TestRuntimeKubeHealthRequiresCurrentGeneration(t *testing.T) {
	for _, tc := range []struct {
		output  string
		healthy bool
	}{
		{"2/2/2/7/7 image", true}, {"2/2/2/7/6 image", false},
		{"2/2/1/7/7 image", false}, {"0/0/0/7/7 image", false}, {"x/x/x/x/x image", false},
	} {
		r := &RuntimeService{KubectlBin: runtimeOutput(t, tc.output)}
		s, err := r.Status(context.Background(), runtimeTestKubeApp(), "web")
		if err != nil || s.Healthy != tc.healthy {
			t.Fatalf("%s: %+v %v", tc.output, s, err)
		}
	}
}

func TestRuntimeReadsRecordedServicesAndPeriodicHealth(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	app := &model.App{ID: "app", Name: "changed-name", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: "edited-next-deployment"}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	r := &RuntimeService{Store: st, DockerBin: runtimeOutput(t, "running|redis@sha256:abc|healthy|run")}
	if state, _, _ := r.Probe(ctx, app); state != model.AppHealthUnknown {
		t.Fatalf("undeployed app: %s", state)
	}
	p := &model.Pipeline{ID: "p", Stages: []model.Stage{{ID: "deploy-web", Type: model.StageTypeDeploy, Config: model.StageConfig{RuntimeName: "original-web-1", ComposeServiceName: "web", DeployRuntime: "compose"}}}}
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.AppDeploys.Create(ctx, &model.AppDeploy{ID: "d", AppID: app.ID, RunID: "run", PipelineID: p.ID, Status: model.RunStatusSuccess}); err != nil {
		t.Fatal(err)
	}
	s, err := r.Status(ctx, app, "web")
	if err != nil || s.Ref != "original-web-1" {
		t.Fatalf("edited app changed observed workload: %+v %v", s, err)
	}
	if _, err := r.Status(ctx, app, "unrelated-container"); err == nil {
		t.Fatal("unrecorded service exposed")
	}
	if err := r.Logs(ctx, app, "unrelated-container", &bytes.Buffer{}); err == nil {
		t.Fatal("unrecorded logs exposed")
	}
	checker := NewAppHealthChecker(st.Apps, WithProber(model.DeployTargetDockerHost, r))
	checker.tick(ctx)
	saved, _ := st.Apps.Get(ctx, app.ID)
	if saved.HealthStatus != model.AppHealthHealthy || saved.HealthCheckedAt == nil {
		t.Fatalf("health not persisted: %+v", saved)
	}
	r.DockerBin = runtimeOutput(t, "running|redis@sha256:abc||run")
	if state, msg, _ := r.Probe(ctx, app); state != model.AppHealthUnknown || !strings.Contains(msg, "no readiness observation") {
		t.Fatalf("running became healthy without check: %s %s", state, msg)
	}
}

func TestRuntimeLogsUseDeploymentEnvironmentGeneration(t *testing.T) {
	ctx := context.Background()
	st, jobs := memory.New(), jobqueue.NewMemory()
	app := &model.App{ID: "app", EnvironmentID: "original"}
	env := &model.Environment{ID: "original", Name: "Original", PlainVars: map[string]string{"MODE": "original"}}
	if err := st.Environments.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	stored, _ := st.Environments.Get(ctx, env.ID)
	payload, _ := json.Marshal(AppDeployPlan{Version: 1, RunID: "run", App: app, EnvironmentVersion: stored.Version})
	if _, err := jobs.Enqueue(ctx, JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:run"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppDeploys.Create(ctx, &model.AppDeploy{ID: "d", AppID: app.ID, RunID: "run"}); err != nil {
		t.Fatal(err)
	}
	r := &RuntimeService{Store: st, SecretGenerationBound: true}
	resolver := &AppEnvResolver{Environments: st.Environments}
	app.EnvironmentID = "changed-app-environment"
	got, err := r.LogEnvironment(ctx, app, jobs, resolver)
	if err != nil || got.Values["MODE"] != "original" {
		t.Fatalf("log masking followed edited App: %+v %v", got, err)
	}
	stored.PlainVars["MODE"] = "rotated"
	if err := st.Environments.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LogEnvironment(ctx, app, jobs, resolver); err == nil {
		t.Fatal("historical logs accepted after secret generation changed")
	}
	if _, err := r.LogEnvironment(ctx, app, nil, resolver); err == nil {
		t.Fatal("unverifiable deployment accepted")
	}
}
