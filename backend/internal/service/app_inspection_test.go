package service

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	ct "github.com/compose-spec/compose-go/v2/types"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
)

const inspectedCompose = `services:
  api:
    build:
      context: ../api
      dockerfile: ../docker/api.Dockerfile
      target: runtime
      args:
        MODE: ${MODE:-release}
    ports: ["${APP_PORT:-8080}:8080"]
    environment:
      DATABASE_URL: postgres://db/development
      API_SECRET: ${SECRET}
      EMPTY: ${OPTIONAL:-}
    depends_on:
      db:
        condition: service_healthy
    healthcheck:
      test: [CMD, /health]
      interval: 10s
    deploy:
      resources:
        limits:
          cpus: '0.75'
          memory: 768m
  db:
    image: postgres:16
    volumes: [data:/var/lib/postgresql/data]
  worker:
    image: alpine:3
    profiles: [worker]
    command: [sleep, infinity]
volumes:
  data: {}
`

func inspectionFixture(t *testing.T, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"deploy/compose.yaml": inspectedCompose, "api/app.txt": "source", "docker/api.Dockerfile": "FROM alpine:3 AS runtime\nARG MODE=private-default\nENV PASSWORD=private-value\nCMD [\"/app\"]\n"}
	for k, v := range extra {
		files[k] = v
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func inspectionApp() *model.App {
	return &model.App{ID: "shop", Name: "shop", GitHubRepo: "acme/shop", Branch: "main", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Files: []string{"deploy/compose.yaml"}}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: "shop-prod"}}
}
func cloudBinding(app *model.App) {
	app.DeployTarget.Kind = model.DeployTargetECS
	app.EnvironmentID = "production"
	app.DeployTarget.ExternalServices = []model.ExternalServiceBinding{{Service: "db", Provider: "gcp-cloud-sql", Resource: "project:region:database", Consumers: []string{"api"}, Environment: map[string]string{"DATABASE_URL": "CLOUD_DATABASE_URL"}}}
}

func TestRepositoryComposeMergeProfilesDockerfileAndNativeRuntime(t *testing.T) {
	root := inspectionFixture(t, map[string]string{"deploy/compose.production.yaml": "services:\n  api:\n    ports: !override [9090:8080]\n    environment:\n      MODE: production\n"})
	app := inspectionApp()
	app.BuildPlan.Files = append(app.BuildPlan.Files, "deploy/compose.production.yaml")
	app.BuildPlan.Profiles = []string{"worker"}
	result, err := loadRepositoryCompose(context.Background(), root, app, map[string]string{"SECRET": "secret$VALUE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) > 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if len(result.Graph.Services) != 3 {
		t.Fatalf("profiles not enabled: %+v", result.Graph)
	}
	api := result.Graph.Services[0]
	if api.Build.Context != "api" || api.Build.Dockerfile != "../docker/api.Dockerfile" || api.Build.Target != "runtime" || api.Build.Args["MODE"] != "release" {
		t.Fatalf("build paths/target lost: %+v", api.Build)
	}
	if api.Ports[0] != "9090:8080" || api.Environment["EMPTY"] != "" {
		t.Fatalf("merge/interpolation wrong: %+v", api)
	}
	if len(result.BuildFiles) != 1 || strings.Contains(result.BuildFiles[0].Content, "private-value") {
		t.Fatalf("unsafe Dockerfile preview: %+v", result.BuildFiles)
	}
	path, err := writeRuntimeCompose(result, app, root, "registry.example.com", 123, synthOpts{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	if strings.HasPrefix(path, root+string(filepath.Separator)) {
		t.Fatal("runtime secrets were written into the build context")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("runtime config permissions %v", info.Mode())
	}
	data, _ := os.ReadFile(path)
	project, err := loader.LoadWithContext(context.Background(), ct.ConfigDetails{WorkingDir: root, ConfigFiles: []ct.ConfigFile{{Filename: path, Content: data}}, Environment: ct.Mapping{"VALUE": "host-must-not-substitute"}})
	if err != nil {
		t.Fatal(err)
	}
	svc := project.Services["api"]
	if svc.Image != "registry.example.com/shop-prod-api:123" || svc.Build != nil || *svc.Environment["API_SECRET"] != "secret$VALUE" || svc.DependsOn["db"].Condition != "service_healthy" {
		t.Fatalf("native runtime differs from review: %+v", svc)
	}
	if project.Volumes["data"].Name != "shop-prod_data" || project.Networks["default"].Name != "shop-prod_default" {
		t.Fatal("resources are not prefix scoped")
	}
}

func TestComposeExternalDatabaseExcludedFromExecutionAndSecretsRedacted(t *testing.T) {
	root := inspectionFixture(t, nil)
	app := inspectionApp()
	cloudBinding(app)
	values := map[string]string{"SECRET": "preview-secret-123", "CLOUD_DATABASE_URL": "postgres://cloud-user:secret@cloud-db/prod"}
	result, err := loadRepositoryCompose(context.Background(), root, app, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) > 0 {
		t.Fatalf("diagnostics: %+v", result.Diagnostics)
	}
	api, db := result.Graph.Services[0], result.Graph.Services[1]
	if !db.External || api.Environment["DATABASE_URL"] != values["CLOUD_DATABASE_URL"] || api.RuntimeSize != "Fargate: 1 vCPU, 2048 MiB" {
		t.Fatalf("binding/sizing mismatch: %+v", api)
	}
	p, _, err := synthesizePipelineFromCompose(app, result.Graph, root, "registry.example.com", 123, "p", "r", nil, synthOpts{appEnv: values})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Stages) != 3 {
		t.Fatalf("external database got execution stages: %+v", p.Stages)
	}
	deploy := p.Stages[2]
	if deploy.Config.DeployRuntime != "ecs" || deploy.Config.RuntimeName != "shop-prod-api" || deploy.Config.Env["DATABASE_URL"] != values["CLOUD_DATABASE_URL"] || deploy.Config.HealthCheck == nil {
		t.Fatalf("ECS dispatch inputs incorrect: %+v", deploy.Config)
	}
	safe := DeploymentViewPipeline(p)
	data, _ := json.Marshal(safe)
	for _, secret := range values {
		if strings.Contains(string(data), secret) {
			t.Fatal("secret in persisted deployment graph")
		}
	}
	if p.Stages[2].Config.Env["DATABASE_URL"] != values["CLOUD_DATABASE_URL"] {
		t.Fatal("redaction mutated live execution")
	}
	if _, err := NewExecutor().Execute(context.Background(), safe, &model.PipelineRun{ID: "replay", PipelineID: p.ID}); err == nil {
		t.Fatal("redacted graph could be replayed")
	}
	redactComposeGraph(result.Graph)
	data, _ = json.Marshal(result.Graph)
	for _, secret := range values {
		if strings.Contains(string(data), secret) || strings.Contains(result.YAML, secret) {
			t.Fatal("secret in Compose preview")
		}
	}
}

func TestComposeInspectionDoesNotReadHostEnvOrEscapingFiles(t *testing.T) {
	t.Setenv("SECRET", "host-process-secret")
	root := inspectionFixture(t, nil)
	app := inspectionApp()
	res, err := loadRepositoryCompose(context.Background(), root, app, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diagnostics) == 0 || res.Graph.Services[0].Environment["API_SECRET"] == "host-process-secret" {
		t.Fatal("host environment used or missing variable accepted")
	}
	for _, scenario := range []string{"compose-symlink", "dockerfile-symlink", "env-file", "include", "extends", "bind-mount"} {
		t.Run(scenario, func(t *testing.T) {
			root := inspectionFixture(t, nil)
			app := inspectionApp()
			outside := filepath.Join(t.TempDir(), "outside")
			os.WriteFile(outside, []byte("SECRET=outside-value\n"), 0600)
			switch scenario {
			case "compose-symlink":
				os.Symlink(outside, filepath.Join(root, "escape.yaml"))
				app.BuildPlan.Files = []string{"escape.yaml"}
			case "dockerfile-symlink":
				os.Remove(filepath.Join(root, "docker/api.Dockerfile"))
				os.Symlink(outside, filepath.Join(root, "docker/api.Dockerfile"))
			case "env-file":
				os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("services:\n  api:\n    image: alpine\n    env_file: "+outside+"\n"), 0600)
				app.BuildPlan.Files = []string{"compose.yaml"}
			case "include":
				os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("include: [https://example.invalid/compose.yaml]\n"), 0600)
				app.BuildPlan.Files = []string{"compose.yaml"}
			case "extends":
				os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("services:\n  api:\n    extends:\n      file: "+outside+"\n      service: api\n"), 0600)
				app.BuildPlan.Files = []string{"compose.yaml"}
			case "bind-mount":
				os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("services:\n  api:\n    image: alpine\n    volumes: [./data:/data]\n"), 0600)
				app.BuildPlan.Files = []string{"compose.yaml"}
			}
			result, err := loadRepositoryCompose(context.Background(), root, app, map[string]string{"SECRET": "provided"})
			if err == nil && len(result.Diagnostics) == 0 {
				t.Fatal("unsafe input was deployable")
			}
		})
	}
}

func TestRepositoryInspectionPinsSourceAndDiscoversNestedFiles(t *testing.T) {
	root := inspectionFixture(t, map[string]string{"services/compose.test.yml": "services:\n  test:\n    image: alpine\n"})
	for _, args := range [][]string{{"init", root}, {"-C", root, "add", "."}, {"-C", root, "-c", "user.name=Cooker test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %s %v", out, err)
		}
	}
	sha, err := github.HeadCommit(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	app := inspectionApp()
	app.BuildPlan.Commit = sha
	app.BuildPlan.InstallationID = 7
	d := NewAppDetectorWithClone(func(_ context.Context, opts github.CloneOptions) (string, error) {
		if opts.Commit != sha || opts.InstallationID != 7 {
			t.Fatal("source identity lost")
		}
		return root, nil
	})
	res, err := d.Inspect(context.Background(), app, map[string]string{"SECRET": "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Commit != sha || len(res.Candidates) != 2 || !res.Deployable {
		t.Fatalf("inspection: %+v", res)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("inspection checkout was retained")
	}
}

func TestAppDeploymentRejectsUnsupportedTargetsBeforeCloning(t *testing.T) {
	for _, kind := range []model.DeployTargetKind{"", "fly", "render", "ssh", "future-cloud"} {
		t.Run(string(kind), func(t *testing.T) {
			d := &AppDeployer{cloneFn: func(context.Context, github.CloneOptions) (string, error) {
				t.Fatal("source cloned before target validation")
				return "", nil
			}}
			a := inspectionApp()
			a.DeployTarget.Kind = kind
			if _, _, err := d.Deploy(context.Background(), a, "r", io.Discard); err == nil {
				t.Fatal("unsupported target accepted")
			}
		})
	}
}

func TestComposeBindingMissingKeyAndUnsupportedFeatureDiagnostics(t *testing.T) {
	for _, scenario := range []string{"missing-key", "consumer", "restart-policy", "extra-build-option"} {
		t.Run(scenario, func(t *testing.T) {
			app := inspectionApp()
			cloudBinding(app)
			files := map[string]string{}
			values := map[string]string{"SECRET": "provided", "CLOUD_DATABASE_URL": "postgres://existing"}
			switch scenario {
			case "missing-key":
				delete(values, "CLOUD_DATABASE_URL")
			case "consumer":
				app.DeployTarget.ExternalServices[0].Consumers = []string{"missing"}
			case "restart-policy":
				files["deploy/compose.yaml"] = strings.Replace(inspectedCompose, "    ports:", "    restart: on-failure\n    ports:", 1)
			case "extra-build-option":
				files["deploy/compose.yaml"] = strings.Replace(inspectedCompose, "      target:", "      network: host\n      target:", 1)
			}
			res, err := loadRepositoryCompose(context.Background(), inspectionFixture(t, files), app, values)
			if err == nil && len(res.Diagnostics) == 0 {
				t.Fatal("unsupported input reported deployable")
			}
		})
	}
}

func TestComposeRejectsGeneratedNamesBeforeCloudMutation(t *testing.T) {
	for _, service := range []string{"api_", strings.Repeat("a", 50)} {
		app := inspectionApp()
		app.DeployTarget.Kind = model.DeployTargetCloudRun
		root := inspectionFixture(t, map[string]string{"deploy/compose.yaml": "services:\n  " + service + ":\n    image: nginx\n"})
		res, err := loadRepositoryCompose(context.Background(), root, app, nil)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, diagnostic := range res.Diagnostics {
			if diagnostic.Field == "prefix" {
				found = true
			}
		}
		if !found {
			t.Fatalf("invalid generated name accepted: %s", service)
		}
	}
}
