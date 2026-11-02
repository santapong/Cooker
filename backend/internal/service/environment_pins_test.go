package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

// Owned metadata/value fixture: no external provider or credentials are used.
type versionedSecretFixture struct {
	*fakeSecrets
	latest                          string
	versions                        map[string]map[string][]byte
	lists, latestReads, pinnedReads int
	err                             error
}

func (f *versionedSecretFixture) List(ctx context.Context, id string) ([]string, error) {
	f.lists++
	return f.fakeSecrets.List(ctx, id)
}
func (f *versionedSecretFixture) Get(ctx context.Context, id, key string) ([]byte, error) {
	f.latestReads++
	return f.fakeSecrets.Get(ctx, id, key)
}
func (f *versionedSecretFixture) Pin(_ context.Context, id, key string) (secrets.VersionReference, error) {
	if f.err != nil {
		return secrets.VersionReference{}, f.err
	}
	return secrets.VersionReference{SchemaVersion: 1, Provider: "vault", Resource: "kv/data/" + id + "/" + key, Version: f.latest, Incarnation: "2026-10-02T12:00:00Z", Scope: secrets.VersionScope{Authority: "https://vault.example.test", Mount: "kv", EnvironmentID: id, Key: key}}, nil
}
func (f *versionedSecretFixture) GetPinned(_ context.Context, id, key string, ref secrets.VersionReference) ([]byte, error) {
	f.pinnedReads++
	if f.err != nil {
		return nil, f.err
	}
	if ref.Scope.EnvironmentID != id || ref.Scope.Key != key {
		return nil, secrets.ErrVersionMismatch
	}
	value, ok := f.versions[ref.Version][key]
	if !ok {
		return nil, secrets.ErrVersionUnavailable
	}
	return slices.Clone(value), nil
}

func immutableSecretFixture(t *testing.T) (*store.Store, *AppEnvResolver, *versionedSecretFixture, *model.Environment) {
	t.Helper()
	st := memory.New()
	env := &model.Environment{ID: "environment", Name: "fixture", PlainVars: map[string]string{"DB_NAME": "public-name"}}
	if err := st.Environments.Create(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	f := &versionedSecretFixture{fakeSecrets: &fakeSecrets{data: map[string]map[string][]byte{env.ID: {"TOKEN": []byte("canary-original-secret"), "OTHER": []byte("canary-other-secret")}}}, latest: "1", versions: map[string]map[string][]byte{"1": {"TOKEN": []byte("canary-original-secret"), "OTHER": []byte("canary-other-secret")}, "2": {"TOKEN": []byte("canary-rotated-secret")}}}
	return st, &AppEnvResolver{Environments: st.Environments, Secrets: f}, f, env
}

func TestSecretPinsResolveOriginalVersionWithoutLatestDiscovery(t *testing.T) {
	_, r, f, env := immutableSecretFixture(t)
	ctx := context.Background()
	pin, err := r.PinEnvironmentSecrets(ctx, env.ID, env.Version, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(pin)
	if strings.Contains(string(data), "canary-") {
		t.Fatal("secret value entered queue metadata")
	}
	f.latest = "2"
	f.data[env.ID]["TOKEN"] = []byte("canary-rotated-secret")
	f.data[env.ID]["NEW"] = []byte("canary-new-secret")
	lists := f.lists
	got, err := r.ResolveDetailed(WithAcceptedSecretPlan(WithEnvironmentVersion(ctx, env.ID, env.Version), pin), &model.App{EnvironmentID: env.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Values["TOKEN"] != "canary-original-secret" || got.Values["DB_NAME"] != "public-name" || got.Values["NEW"] != "" || f.lists != lists || f.latestReads != 0 || f.pinnedReads != 2 {
		t.Fatal("execution used unaccepted values or discovered latest keys")
	}
}

func TestSecretPinsExplicitEmptyAndLegacyAbsenceAreDifferent(t *testing.T) {
	_, r, f, env := immutableSecretFixture(t)
	f.data[env.ID] = map[string][]byte{}
	pin, err := r.PinEnvironmentSecrets(context.Background(), env.ID, env.Version, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(pin)
	if !strings.Contains(string(data), `"keys":[]`) {
		t.Fatal("empty admission evidence was lost")
	}
	f.data[env.ID]["TOKEN"] = []byte("future-canary")
	lists := f.lists
	got, err := r.ResolveDetailed(WithAcceptedSecretPlan(context.Background(), pin), &model.App{EnvironmentID: env.ID})
	if err != nil || got.Values["TOKEN"] != "" || f.lists != lists {
		t.Fatal("empty accepted key set changed")
	}
	if _, err := r.ResolveDetailed(WithAcceptedSecretPlan(context.Background(), nil), &model.App{EnvironmentID: env.ID}); err == nil {
		t.Fatal("missing legacy metadata authorized latest external reads")
	}
}

func TestSecretPinsRejectUnsupportedDriftAndUnsafeProviderErrors(t *testing.T) {
	st, r, f, env := immutableSecretFixture(t)
	pin, err := r.PinEnvironmentSecrets(context.Background(), env.ID, env.Version, []string{"TOKEN"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"unsupported", "unavailable", "generation", "scope", "extra"} {
		t.Run(mode, func(t *testing.T) {
			copyPlan := *pin
			copyPlan.References = map[string]secrets.VersionReference{"TOKEN": pin.References["TOKEN"]}
			resolver := *r
			switch mode {
			case "unsupported":
				resolver.Secrets = f.fakeSecrets
			case "unavailable":
				f.err = fmt.Errorf("private provider error canary-original-secret")
				defer func() { f.err = nil }()
			case "generation":
				copyPlan.EnvironmentVersion++
			case "scope":
				ref := copyPlan.References["TOKEN"]
				ref.Scope.Key = "OTHER"
				copyPlan.References["TOKEN"] = ref
			case "extra":
				copyPlan.References["OTHER"] = pin.References["TOKEN"]
			}
			_, err := resolver.ResolveDetailed(WithAcceptedSecretPlan(context.Background(), &copyPlan), &model.App{EnvironmentID: env.ID})
			if err == nil || strings.Contains(err.Error(), "canary-") {
				t.Fatal("unverified read or private error exposed")
			}
		})
	}
	unsupported := *r
	unsupported.Secrets = f.fakeSecrets
	if _, err := unsupported.PinEnvironmentSecrets(context.Background(), env.ID, env.Version, nil, true); err == nil {
		t.Fatal("unversioned backend admitted a plan")
	}
	if err := st.Environments.Update(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolveDetailed(WithAcceptedSecretPlan(context.Background(), pin), &model.App{EnvironmentID: env.ID}); err == nil {
		t.Fatal("changed plain config accepted")
	}
}

func TestSecretPinsAppReceiptAndHistoricalLogsRetainVersions(t *testing.T) {
	st, r, f, env := immutableSecretFixture(t)
	app := &model.App{ID: "app", Name: "fixture", GitHubRepo: "owned/fixture", Branch: "main", EnvironmentID: env.ID, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := st.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewMemory()
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: &AppDeployer{EnvResolver: r}, RequireSecretPins: true}
	commit := strings.Repeat("a", 40)
	receipt, err := q.EnqueueIntent(context.Background(), app, commit, nil, "intent")
	if err != nil {
		t.Fatal(err)
	}
	job, _ := jobs.Get(context.Background(), receipt.JobID)
	if strings.Contains(string(job.Payload), "canary-") {
		t.Fatal("plaintext persisted")
	}
	f.latest = "2"
	f.err = fmt.Errorf("private provider error")
	retry, err := q.EnqueueIntent(context.Background(), app, commit, nil, "intent")
	if err != nil || retry.PlanDigest != receipt.PlanDigest {
		t.Fatal("receipt retry attempted to repin accepted versions")
	}
	f.err = nil
	got, err := deploymentLogEnvironment(context.Background(), app.ID, &model.AppDeploy{AppID: app.ID, RunID: receipt.RunID}, jobs, r, true)
	if err != nil || got.Values["TOKEN"] != "canary-original-secret" {
		t.Fatal("historical log context did not retain secret identity")
	}
	delete(f.versions, "1")
	if _, err := deploymentLogEnvironment(context.Background(), app.ID, &model.AppDeploy{AppID: app.ID, RunID: receipt.RunID}, jobs, r, true); err == nil {
		t.Fatal("deleted historical version authorized log replay")
	}
}

func TestSecretPinsRuntimeHelperRejectsLegacyExternalReplay(t *testing.T) {
	st, resolver, manager, env := immutableSecretFixture(t)
	app := &model.App{ID: "app", EnvironmentID: env.ID}
	jobs := jobqueue.NewMemory()
	data, _ := json.Marshal(AppDeployPlan{Version: 1, RunID: "legacy", App: app, EnvironmentVersion: env.Version})
	if _, err := jobs.Enqueue(context.Background(), JobKindAppDeploy, data, jobqueue.EnqueueOptions{ID: "app-run:legacy"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppDeploys.Create(context.Background(), &model.AppDeploy{ID: "deployment", AppID: app.ID, RunID: "legacy"}); err != nil {
		t.Fatal(err)
	}
	runtime := &RuntimeService{Store: st}
	if _, err := runtime.LogEnvironment(context.Background(), app, jobs, resolver); err == nil || manager.latestReads != 0 || manager.pinnedReads != 0 {
		t.Fatal("legacy helper authorized unverifiable secret replay")
	}
}
