package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	ct "github.com/compose-spec/compose-go/v2/types"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func scopedFixture(t *testing.T, content string) (string, *model.App, context.Context, map[string]string) {
	t.Helper()
	root := inspectionFixture(t, map[string]string{"compose.yaml": content, "Dockerfile": "FROM scratch\n"})
	app := inspectionApp()
	app.BuildPlan.Files = []string{"compose.yaml"}
	app.BuildPlan.EnvironmentScope = ServiceEnvironmentScope
	app.BuildPlan.ServiceEnvironment = map[string][]string{"web": {"DB_PASSWORD", "DB_NAME"}, "worker": {"DB_PASSWORD"}}
	values := map[string]string{"DB_PASSWORD": "scope-test$secret", "DB_NAME": "ordinary_database", "UNUSED_PASSWORD": "unrelated-secret-value"}
	return root, app, WithLogSecrets(context.Background(), []string{values["DB_PASSWORD"], values["UNUSED_PASSWORD"]}), values
}

func TestServiceEnvironmentScopeIsolatesRuntimeAndKeepsBuildsClean(t *testing.T) {
	root, app, ctx, values := scopedFixture(t, `services:
  web:
    build: {context: ., args: {MODE: release}}
    environment:
      CONNECTION_PASSWORD: ${DB_PASSWORD}
      DATABASE_NAME: ${DB_NAME}
  worker:
    image: alpine:3
  redis:
    image: redis:7
`)
	resolved, err := loadRepositoryCompose(ctx, root, app, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Diagnostics) > 0 {
		t.Fatalf("unexpected diagnostic: %+v", resolved.Diagnostics)
	}
	p, _, err := synthesizePipelineFromCompose(app, resolved.Graph, root, "registry.test", 1, "p", "r", nil, synthOpts{appEnv: values})
	if err != nil {
		t.Fatal(err)
	}
	web, worker, redis := stageByID(p, "deploy-web"), stageByID(p, "deploy-worker"), stageByID(p, "deploy-redis")
	if web.Config.Env["CONNECTION_PASSWORD"] != values["DB_PASSWORD"] || web.Config.Env["DB_NAME"] != values["DB_NAME"] || worker.Config.Env["DB_PASSWORD"] != values["DB_PASSWORD"] || len(redis.Config.Env) != 0 {
		t.Fatal("per-service scope was not applied")
	}
	for _, stage := range p.Stages {
		if _, ok := stage.Config.Env["UNUSED_PASSWORD"]; ok {
			t.Fatal("unselected secret injected")
		}
		if stage.Type == model.StageTypeBuild && (len(stage.Config.Env) > 0 || len(stage.Config.BuildArgs) != 1 || stage.Config.BuildArgs["MODE"] != "release") {
			t.Fatal("runtime environment leaked into build")
		}
	}
	file, err := writeRuntimeCompose(resolved, app, root, "registry.test", 1, synthOpts{appEnv: values})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	project, err := loader.LoadWithContext(ctx, ct.ConfigDetails{WorkingDir: root, ConfigFiles: []ct.ConfigFile{{Filename: file, Content: data}}, Environment: ct.Mapping{}}, func(o *loader.Options) { o.SetProjectName("scope", true) })
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Services["redis"].Environment) != 0 || *project.Services["worker"].Environment["DB_PASSWORD"] != values["DB_PASSWORD"] {
		t.Fatal("native Compose bypassed per-service scope")
	}
	if strings.Contains(string(data), values["UNUSED_PASSWORD"]) {
		t.Fatal("unused secret leaked into runtime file")
	}
}

func TestServiceEnvironmentScopeRejectsBypasses(t *testing.T) {
	tests := map[string]string{
		"unlisted interpolation": "services:\n  web: {image: alpine:3}\n  worker: {image: alpine:3}\n  redis: {image: redis:7, environment: {ALIAS: '${DB_PASSWORD}'}}\n",
		"bare inheritance":       "services:\n  web: {image: alpine:3}\n  worker: {image: alpine:3}\n  redis: {image: redis:7, environment: [DB_PASSWORD]}\n",
		"literal alias":          "services:\n  web: {image: alpine:3}\n  worker: {image: alpine:3}\n  redis: {image: redis:7, environment: {ALIAS: 'scope-test$$secret'}}\n",
		"build interpolation":    "services:\n  web: {build: {context: ., args: {PASSWORD: '${DB_PASSWORD}'}}}\n  worker: {image: alpine:3}\n",
		"build bare argument":    "services:\n  web: {build: {context: ., args: [DB_PASSWORD]}}\n  worker: {image: alpine:3}\n",
		"build literal value":    "services:\n  web: {build: {context: ., args: {ALIAS: 'scope-test$$secret'}}}\n  worker: {image: alpine:3}\n",
		"command interpolation":  "services:\n  web: {image: alpine:3, command: ['echo','${DB_PASSWORD}']}\n  worker: {image: alpine:3}\n",
		"command literal":        "services:\n  web: {image: alpine:3, command: ['echo','scope-test$$secret']}\n  worker: {image: alpine:3}\n",
		"env file":               "services:\n  web: {image: alpine:3, env_file: scoped.env}\n  worker: {image: alpine:3}\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			root, app, ctx, values := scopedFixture(t, content)
			if _, err := loadRepositoryCompose(ctx, root, app, values); err == nil {
				t.Fatal("scope bypass was accepted")
			}
		})
	}
}

func TestServiceEnvironmentScopeRejectsUnknownKeysServicesAndCollisions(t *testing.T) {
	for _, kind := range []string{"unknown service", "unknown key", "duplicate key", "interpolation collision", "invalid mode", "map without mode"} {
		t.Run(kind, func(t *testing.T) {
			root, app, ctx, values := scopedFixture(t, "services:\n  web: {image: alpine:3}\n  worker: {image: alpine:3}\n")
			switch kind {
			case "unknown service":
				app.BuildPlan.ServiceEnvironment["ghost"] = []string{"DB_NAME"}
			case "unknown key":
				app.BuildPlan.ServiceEnvironment["web"] = []string{"MISSING"}
			case "duplicate key":
				app.BuildPlan.ServiceEnvironment["web"] = []string{"DB_NAME", "DB_NAME"}
			case "interpolation collision":
				app.BuildPlan.Variables = map[string]string{"DB_PASSWORD": "public override"}
			case "invalid mode":
				app.BuildPlan.EnvironmentScope = "typo"
			case "map without mode":
				app.BuildPlan.EnvironmentScope = ""
			}
			if _, err := loadRepositoryCompose(ctx, root, app, values); err == nil {
				t.Fatal("invalid scope accepted")
			}
		})
	}
}

func TestServiceEnvironmentScopeEmptyMapSurvivesSerialization(t *testing.T) {
	root, app, ctx, values := scopedFixture(t, "services:\n  redis: {image: redis:7}\n")
	app.BuildPlan.ServiceEnvironment = map[string][]string{}
	copy := cloneScopeApp(t, app)
	resolved, err := loadRepositoryCompose(ctx, root, copy, values)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := synthesizePipelineFromCompose(copy, resolved.Graph, root, "registry.test", 1, "p", "r", nil, synthOpts{appEnv: values})
	if err != nil {
		t.Fatal(err)
	}
	if len(stageByID(p, "deploy-redis").Config.Env) != 0 {
		t.Fatal("empty map became legacy global injection")
	}
}

func cloneScopeApp(t *testing.T, app *model.App) *model.App {
	t.Helper()
	return store.CloneTrigger(app)
}

func TestServiceEnvironmentScopeRejectsPlainSecretCollision(t *testing.T) {
	st := memory.New()
	ctx := WithLogSecrets(context.Background(), nil)
	if err := st.Environments.Create(ctx, &model.Environment{ID: "env", Name: "env", PlainVars: map[string]string{"TOKEN": "public"}}); err != nil {
		t.Fatal(err)
	}
	r := &AppEnvResolver{Environments: st.Environments, Secrets: &fakeSecrets{data: map[string]map[string][]byte{"env": {"TOKEN": []byte("secret-value")}}}}
	app := &model.App{EnvironmentID: "env", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, EnvironmentScope: ServiceEnvironmentScope}}
	if _, err := r.Resolve(ctx, app); err == nil {
		t.Fatal("plain/secret collision accepted")
	}
	app.BuildPlan = nil
	if values, err := r.Resolve(ctx, app); err != nil || values["TOKEN"] != "secret-value" {
		t.Fatal("legacy precedence changed")
	}
	if RedactLogText(ctx, "secret-value") != "[REDACTED]" {
		t.Fatal("inspection context missed resolved secret")
	}
}
