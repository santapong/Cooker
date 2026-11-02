package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestN8nQueueExampleComposeReviewAndDAG(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	app := &model.App{ID: "n8n-example", Name: "n8n queue", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Files: []string{"docs/examples/n8n-queue/compose.yaml"}}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: "cooker-n8n-example"}}
	values := map[string]string{"POSTGRES_PASSWORD": "test-only-not-a-real-secret", "N8N_ENCRYPTION_KEY": "test-only-same-key-for-main-and-worker", "N8N_IMAGE": "n8nio/n8n:2.28.6", "N8N_PORT": "19568"}
	resolved, err := loadRepositoryCompose(context.Background(), root, app, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Diagnostics) > 0 {
		t.Fatalf("unsupported Compose settings: %+v", resolved.Diagnostics)
	}
	if len(resolved.Graph.Services) != 4 {
		t.Fatalf("expected four services, got %d", len(resolved.Graph.Services))
	}
	for _, svc := range resolved.Graph.Services {
		if svc.Build != nil {
			t.Fatal("prebuilt service unexpectedly requires a build")
		}
	}
	main := resolved.Project.Services["n8n-main"]
	worker := resolved.Project.Services["n8n-worker"]
	if *main.Environment["EXECUTIONS_MODE"] != "queue" || *worker.Environment["EXECUTIONS_MODE"] != "queue" {
		t.Fatal("queue setting lost")
	}
	if *main.Environment["N8N_ENCRYPTION_KEY"] != *worker.Environment["N8N_ENCRYPTION_KEY"] {
		t.Fatal("encryption keys diverged")
	}
	if len(worker.Command) != 2 || worker.Command[0] != "worker" || worker.Command[1] != "--concurrency=5" {
		t.Fatal("worker command lost")
	}
	if worker.DependsOn["n8n-main"].Condition != "service_healthy" {
		t.Fatal("migration/readiness dependency lost")
	}
	if len(main.Ports) != 1 || main.Ports[0].HostIP != "127.0.0.1" {
		t.Fatal("loopback binding lost")
	}
	runtime, err := writeRuntimeCompose(resolved, app, root, "unused.example", 123, synthOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(runtime)
	data, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "service_healthy") || !strings.Contains(string(data), "--concurrency=5") {
		t.Fatal("runtime Compose dropped health or worker configuration")
	}
	pipeline, _, err := synthesizePipelineFromCompose(app, resolved.Graph, root, "unused.example", 123, "n8n-example-pipeline", "n8n-example-run", nil, synthOpts{composeFile: runtime})
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline.Stages) != 4 {
		t.Fatalf("expected four deployment stages, got %d", len(pipeline.Stages))
	}
	for _, stage := range pipeline.Stages {
		if stage.Type != model.StageTypeDeploy || stage.Config.DeployRuntime != "compose" {
			t.Fatalf("wrong stage type/runtime: %s %s", stage.Type, stage.Config.DeployRuntime)
		}
	}
	edges := edgeSet(pipeline)
	for _, edge := range []string{"deploy-postgres->deploy-n8n-main", "deploy-redis->deploy-n8n-main", "deploy-postgres->deploy-n8n-worker", "deploy-redis->deploy-n8n-worker", "deploy-n8n-main->deploy-n8n-worker"} {
		if !edges[edge] {
			t.Errorf("missing edge %s", edge)
		}
	}
	if len(edges) != 5 {
		t.Fatalf("expected five dependencies, got %d", len(edges))
	}
	t.Log("PASS: four image-only services, healthy dependency conditions, worker command, shared encryption key, loopback port, native Compose runtime, four deployment stages and five dependency edges")
}
