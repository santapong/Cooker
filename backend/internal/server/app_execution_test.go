package server

import (
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/model"
	"testing"
)

func TestAppExecutionRequiresActualCompatibleBackends(t *testing.T) {
	app := &model.App{DeployTarget: model.DeployTarget{Kind: model.DeployTargetECS}}
	for _, cfg := range []*config.Config{{}, {BuilderBackend: "docker", PusherBackend: "noop"}, {BuilderBackend: "buildkit", PusherBackend: "crane"}, {BuilderBackend: "kaniko", PusherBackend: "crane"}} {
		if err := appExecutionCheck(cfg)(app, true); err == nil {
			t.Fatalf("nonfunctional source/image handoff accepted: %+v", cfg)
		}
	}
	if err := appExecutionCheck(&config.Config{BuilderBackend: "docker", PusherBackend: "docker"})(app, true); err != nil {
		t.Fatal(err)
	}
	app.DeployTarget.Kind = model.DeployTargetKubernetes
	if err := appExecutionCheck(&config.Config{})(app, false); err == nil {
		t.Fatal("no-op Kubernetes deployer accepted")
	}
}

func TestAppExecutionBuildKitCraneConfigured(t *testing.T) {
	app := &model.App{DeployTarget: model.DeployTarget{Kind: model.DeployTargetECS}}
	cfg := &config.Config{BuilderBackend: "buildkit", PusherBackend: "crane", BuildKit: config.BuildKitConfig{Address: "unix:///run/buildkit/buildkitd.sock"}}
	if err := appExecutionCheck(cfg)(app, true); err != nil {
		t.Fatal(err)
	}
	cfg.Env = config.EnvProduction
	cfg.BuilderBackend = "docker"
	cfg.PusherBackend = "docker"
	if err := appExecutionCheck(cfg)(app, true); err == nil {
		t.Fatal("production Docker socket build was advertised")
	}
}

func TestAppSourceBuildRequiresImmutableHandoffOutputs(t *testing.T) {
	t.Setenv("COOKER_OUTPUTS_ENABLED", "false")
	cfg := &config.Config{BuilderBackend: "buildkit", PusherBackend: "crane", BuildKit: config.BuildKitConfig{Address: "unix:///fixture.sock"}}
	app := &model.App{DeployTarget: model.DeployTarget{Kind: model.DeployTargetECS}}
	if err := appExecutionCheck(cfg)(app, true); err == nil {
		t.Fatal("source build advertised without immutable output interpolation")
	}
	if err := appExecutionCheck(cfg)(app, false); err != nil {
		t.Fatalf("image-only deployment unnecessarily blocked: %v", err)
	}
}
