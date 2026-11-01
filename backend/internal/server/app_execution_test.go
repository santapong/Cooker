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
