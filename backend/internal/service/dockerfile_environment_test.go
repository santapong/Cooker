package service

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/source/github"
)

func dockerfileScopedApp(envID string, keys ...string) *model.App {
	return &model.App{ID: "scoped-image", Name: "Scoped image", GitHubRepo: "fixture/image", Branch: "main", EnvironmentID: envID,
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost},
		BuildPlan:    &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile", EnvironmentScope: ServiceEnvironmentScope, ServiceEnvironment: map[string][]string{DockerfileRuntimeService: keys}, Args: map[string]string{"MODE": "release"}}}
}

func TestDockerfileEnvironmentAdmissionCapturesSubsetAndRecoversReceipt(t *testing.T) {
	st, resolver, provider, env := immutableSecretFixture(t)
	app := dockerfileScopedApp(env.ID, "TOKEN", "DB_NAME")
	if err := st.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewMemory()
	q := &AppDeployQueue{Store: st, Jobs: jobs, Deployer: &AppDeployer{EnvResolver: resolver}, RequireSecretPins: true}
	receipt, err := q.EnqueueIntent(context.Background(), app, strings.Repeat("a", 40), nil, "scoped-intent")
	if err != nil {
		t.Fatal(err)
	}
	job, _ := jobs.Get(context.Background(), receipt.JobID)
	var accepted AppDeployPlan
	if json.Unmarshal(job.Payload, &accepted) != nil || accepted.Secrets == nil || !reflect.DeepEqual(accepted.Secrets.Keys, []string{"TOKEN"}) || accepted.EnvironmentVersion != env.Version {
		t.Fatal("subset was not captured")
	}
	if strings.Contains(string(job.Payload), "canary-") || len(accepted.Secrets.References) != 1 {
		t.Fatal("private values or extra refs entered accepted metadata")
	}
	// A new resolver restores the JSONB-shaped plan, never discovers latest keys.
	provider.latest = "2"
	provider.data[env.ID]["TOKEN"] = []byte("canary-rotated-secret")
	provider.data[env.ID]["NEW"] = []byte("canary-new-secret")
	lists := provider.lists
	fresh := &AppEnvResolver{Environments: st.Environments, Secrets: provider}
	ctx := WithAcceptedSecretPlan(WithEnvironmentVersion(context.Background(), env.ID, accepted.EnvironmentVersion), accepted.Secrets)
	values, err := fresh.ResolveDetailed(ctx, accepted.App)
	if err != nil || !reflect.DeepEqual(values.Values, map[string]string{"TOKEN": "canary-original-secret", "DB_NAME": "public-name"}) || provider.pinnedReads != 1 || provider.latestReads != 0 || provider.lists != lists {
		t.Fatal("restored execution broadened or refreshed authority")
	}
	app.BuildPlan.ServiceEnvironment[DockerfileRuntimeService] = []string{"OTHER"}
	freshQueue := &AppDeployQueue{Jobs: jobs}
	recovered, err := freshQueue.EnqueueIntent(context.Background(), app, strings.Repeat("a", 40), nil, "scoped-intent")
	if err != nil || *recovered != *receipt || provider.lists != lists {
		t.Fatal("receipt retry repinned runtime keys")
	}
}

func TestDockerfileEnvironmentEmptySelectionIsDurableAndDoesNotDiscoverSecrets(t *testing.T) {
	for _, kind := range []string{"empty-list", "empty-map", "nil-map"} {
		t.Run(kind, func(t *testing.T) {
			st, resolver, provider, env := immutableSecretFixture(t)
			app := dockerfileScopedApp(env.ID)
			if kind == "empty-map" {
				app.BuildPlan.ServiceEnvironment = map[string][]string{}
			}
			if kind == "nil-map" {
				app.BuildPlan.ServiceEnvironment = nil
			}
			if err := st.Apps.Create(context.Background(), app); err != nil {
				t.Fatal(err)
			}
			q := &AppDeployQueue{Store: st, Jobs: jobqueue.NewMemory(), Deployer: &AppDeployer{EnvResolver: resolver}, RequireSecretPins: true}
			receipt, err := q.Enqueue(context.Background(), app, strings.Repeat("a", 40), nil)
			if err != nil {
				t.Fatal(err)
			}
			job, _ := q.Jobs.Get(context.Background(), receipt.JobID)
			var accepted AppDeployPlan
			if json.Unmarshal(job.Payload, &accepted) != nil || accepted.Secrets == nil || accepted.Secrets.Keys == nil || len(accepted.Secrets.Keys) != 0 || accepted.Secrets.Mode != "none" {
				t.Fatal("explicit absence became missing evidence")
			}
			got, err := resolver.ResolveDetailed(WithAcceptedSecretPlan(context.Background(), accepted.Secrets), accepted.App)
			if err != nil || len(got.Values) != 0 || len(got.SecretValues) != 0 || provider.lists != 0 || provider.latestReads != 0 || provider.pinnedReads != 0 {
				t.Fatal("empty runtime scope discovered or injected values")
			}
		})
	}
}

func TestDockerfileEnvironmentInvalidNamesFailBeforeSourceOrValueReads(t *testing.T) {
	for _, kind := range []string{"unknown-key", "wrong-component", "duplicate-key", "plain-secret-collision", "build-key", "variables", "no-environment", "buildpack"} {
		t.Run(kind, func(t *testing.T) {
			st, resolver, provider, env := immutableSecretFixture(t)
			app := dockerfileScopedApp(env.ID, "TOKEN")
			switch kind {
			case "unknown-key":
				app.BuildPlan.ServiceEnvironment[DockerfileRuntimeService] = []string{"MISSING"}
			case "wrong-component":
				app.BuildPlan.ServiceEnvironment = map[string][]string{"web": {"TOKEN"}}
			case "duplicate-key":
				app.BuildPlan.ServiceEnvironment[DockerfileRuntimeService] = []string{"TOKEN", "TOKEN"}
			case "plain-secret-collision":
				env.PlainVars["TOKEN"] = "public"
			case "build-key":
				app.BuildPlan.Args["TOKEN"] = "public-build-input"
			case "variables":
				app.BuildPlan.Variables = map[string]string{"MODE": "release"}
			case "no-environment":
				app.EnvironmentID = ""
			case "buildpack":
				app.BuildPlan.Kind = model.BuildPlanBuildpack
			}
			if err := st.Apps.Create(context.Background(), app); err != nil {
				t.Fatal(err)
			}
			resolutions := 0
			q := &AppDeployQueue{Store: st, Jobs: jobqueue.NewMemory(), Deployer: &AppDeployer{EnvResolver: resolver}, ResolveCommit: func(context.Context, int64, string, string) (string, error) {
				resolutions++
				return strings.Repeat("a", 40), nil
			}}
			if _, err := q.Enqueue(context.Background(), app, "", nil); err == nil {
				t.Fatal("invalid runtime binding admitted")
			}
			if resolutions != 0 || provider.latestReads != 0 || provider.pinnedReads != 0 {
				t.Fatal("invalid binding reached source or value retrieval")
			}
		})
	}
}

func TestDockerfileEnvironmentAcceptedEvidenceCannotBroadenOrRepin(t *testing.T) {
	for _, kind := range []string{"missing", "extra-key", "wrong-key", "missing-ref", "wrong-ref", "environment-drift", "selection-drift"} {
		t.Run(kind, func(t *testing.T) {
			st, resolver, provider, env := immutableSecretFixture(t)
			app := dockerfileScopedApp(env.ID, "TOKEN", "DB_NAME")
			pin, err := resolver.pinDockerfileEnvironment(context.Background(), app, env)
			if err != nil {
				t.Fatal(err)
			}
			version := env.Version
			switch kind {
			case "missing":
				pin = nil
			case "extra-key":
				pin.Keys = append(pin.Keys, "OTHER")
				ref, _ := provider.Pin(context.Background(), env.ID, "OTHER")
				pin.References["OTHER"] = ref
			case "wrong-key":
				pin.Keys = []string{"OTHER"}
				ref, _ := provider.Pin(context.Background(), env.ID, "OTHER")
				pin.References = map[string]secrets.VersionReference{"OTHER": ref}
			case "missing-ref":
				delete(pin.References, "TOKEN")
			case "wrong-ref":
				ref := pin.References["TOKEN"]
				ref.Scope.Key = "OTHER"
				pin.References["TOKEN"] = ref
			case "environment-drift":
				if err := st.Environments.Update(context.Background(), env); err != nil {
					t.Fatal(err)
				}
			case "selection-drift":
				app.BuildPlan.ServiceEnvironment[DockerfileRuntimeService] = []string{"OTHER"}
			}
			lists := provider.lists
			ctx := WithAcceptedSecretPlan(WithEnvironmentVersion(context.Background(), env.ID, version), pin)
			if _, err := resolver.ResolveDetailed(ctx, app); err == nil {
				t.Fatal("invalid accepted evidence resolved")
			}
			if provider.lists != lists || provider.latestReads != 0 || provider.pinnedReads != 0 {
				t.Fatal("worker discovered or retrieved unaccepted values")
			}
		})
	}
}

func TestDockerfileEnvironmentProtectsBuildArgsBeforeCheckoutAndRuntime(t *testing.T) {
	_, resolver, provider, env := immutableSecretFixture(t)
	app := dockerfileScopedApp(env.ID, "TOKEN")
	app.BuildPlan.Args["ALIAS"] = "canary-original-secret"
	clones := 0
	d := &AppDeployer{EnvResolver: resolver, cloneFn: func(context.Context, github.CloneOptions) (string, error) { clones++; return "", nil }}
	for _, kind := range []string{"deploy", "images", "build-push"} {
		t.Run(kind, func(t *testing.T) {
			var err error
			switch kind {
			case "deploy":
				_, _, err = d.Deploy(context.Background(), app, "run", io.Discard)
			case "images":
				pin, _ := resolver.pinDockerfileEnvironment(context.Background(), app, env)
				_, err = d.PrepareImages(context.Background(), &AppDeployPlan{App: app, Commit: strings.Repeat("a", 40), EnvironmentVersion: env.Version, Secrets: pin}, nil)
			case "build-push":
				_, _, _, err = d.BuildAndPushImage(context.Background(), app, "run", io.Discard)
			}
			if err == nil || strings.Contains(err.Error(), "canary-") || clones != 0 {
				t.Fatal("protected build input reached checkout or public error")
			}
		})
	}
	if provider.latestReads != 0 {
		t.Fatal("immutable secret capability was bypassed")
	}
}

func TestDockerfileEnvironmentProjectsOnlyRuntimeKeysAndPreservesLegacy(t *testing.T) {
	app := dockerfileScopedApp("environment", "TOKEN", "DB_NAME")
	all := map[string]string{"TOKEN": "selected-private", "DB_NAME": "database", "OTHER": "unrelated-private"}
	p, _ := synthesizePipeline(app, app.BuildPlan, "/owned/work", "registry.test/owned:tag", nil, synthOpts{appEnv: all})
	if !reflect.DeepEqual(stageByID(p, "deploy").Config.Env, map[string]string{"TOKEN": "selected-private", "DB_NAME": "database"}) {
		t.Fatal("single runtime injection ignored selected keys")
	}
	for _, s := range p.Stages {
		if s.Type != model.StageTypeDeploy && (len(s.Config.Env) != 0 || s.Config.BuildArgs["TOKEN"] != "") {
			t.Fatal("runtime keys became build/push inputs")
		}
	}
	app.BuildPlan.EnvironmentScope = ""
	app.BuildPlan.ServiceEnvironment = nil
	p, _ = synthesizePipeline(app, app.BuildPlan, "/owned/work", "registry.test/owned:tag", nil, synthOpts{appEnv: all})
	if !reflect.DeepEqual(stageByID(p, "deploy").Config.Env, all) {
		t.Fatal("legacy omitted scope changed")
	}
}

func TestDockerfileEnvironmentHistoricalLogsUseOriginalSubsetAndGeneration(t *testing.T) {
	st, resolver, provider, env := immutableSecretFixture(t)
	app := dockerfileScopedApp(env.ID, "TOKEN")
	if err := st.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewMemory()
	q := &AppDeployQueue{Store: st, Jobs: jobs, Deployer: &AppDeployer{EnvResolver: resolver}, RequireSecretPins: true}
	receipt, err := q.Enqueue(context.Background(), app, strings.Repeat("a", 40), nil)
	if err != nil {
		t.Fatal(err)
	}
	deployed := &model.AppDeploy{AppID: app.ID, RunID: receipt.RunID}
	app.BuildPlan.ServiceEnvironment[DockerfileRuntimeService] = []string{"OTHER"}
	lists := provider.lists
	values, err := deploymentLogEnvironment(context.Background(), app.ID, deployed, jobs, resolver)
	if err != nil || len(values.SecretValues) != 1 || values.Values["TOKEN"] != "canary-original-secret" || len(values.Values) != 1 || provider.lists != lists {
		t.Fatal("log redaction adopted current key selection")
	}
	if err := st.Environments.Update(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	reads := provider.pinnedReads
	if _, err := deploymentLogEnvironment(context.Background(), app.ID, deployed, jobs, resolver); err == nil || provider.pinnedReads != reads {
		t.Fatal("historical logs used a changed environment generation")
	}
}
