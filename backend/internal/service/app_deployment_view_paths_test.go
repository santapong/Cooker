package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestDeploymentViewDoesNotPersistPrivateCheckoutContext(t *testing.T) {
	const checkout = "/private-checkout-canary/owned-workdir"
	app := &model.App{
		ID: "owned-app", Name: "Owned app",
		BuildPlan:    &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "docker/Dockerfile"},
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: "owned-app"},
	}
	execution, _ := synthesizePipeline(app, app.BuildPlan, checkout, "registry.example.invalid/owned:release", nil, synthOpts{})
	view := DeploymentViewPipeline(execution)
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), checkout) {
		t.Fatal("private checkout context entered the persisted deployment graph")
	}
	if execution.Stages[0].Config.Context != checkout {
		t.Fatal("presentation scrubbing changed the executable build context")
	}
	if view.Stages[0].Config.Dockerfile != "docker/Dockerfile" {
		t.Fatal("review graph lost the repository-relative Dockerfile")
	}
	for _, stage := range view.Stages {
		if !stage.Config.ReviewOnly {
			t.Fatal("persisted App graph gained execution authority")
		}
	}
}
