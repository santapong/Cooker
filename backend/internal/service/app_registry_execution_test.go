package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/pusher"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func appRegistryQueueFixture(t *testing.T) (*AppDeployQueue, *model.App, *registryVersionFixture, *model.RegistryConfig) {
	t.Helper()
	st, authority, values, config := registryAuthorityFixture(t)
	app := &model.App{ID: "app-a", Name: "owned-app", GitHubRepo: "owned/fixture", Branch: "main", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile", RegistryConnectionIDs: []string{config.ID}}}
	if err := st.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	d := NewAppDeployer(NewExecutor(WithRegistryAuthority(authority)), "ghcr.io/team")
	q := &AppDeployQueue{Jobs: jobqueue.NewMemory(), Store: st, Deployer: d, RegistryAuthority: authority, RequireRegistryPins: true, AdmissionGuard: memory.NewTriggerStore(), RequireImagePins: true,
		PrepareImages: func(context.Context, *AppDeployPlan) (*AppImageResolution, error) {
			return &AppImageResolution{Version: 1, Kind: model.BuildPlanDockerfile, Services: map[string]AppImagePin{}}, nil
		},
	}
	return q, app, values, config
}

func TestAppRegistryDeniedBeforeSourceImagesAndCredentials(t *testing.T) {
	for _, name := range []string{"ungranted", "duplicate-selection", "private-environment", "missing-authority", "missing-guard", "imported-app"} {
		t.Run(name, func(t *testing.T) {
			q, app, f, _ := appRegistryQueueFixture(t)
			calls := 0
			q.ResolveSource = func(context.Context, *model.App, string) (string, error) {
				calls++
				return strings.Repeat("a", 40), nil
			}
			q.PrepareImages = func(context.Context, *AppDeployPlan) (*AppImageResolution, error) {
				calls++
				return nil, errors.New("must not prepare images")
			}
			switch name {
			case "ungranted":
				app.ID = "app-b"
			case "duplicate-selection":
				app.BuildPlan.RegistryConnectionIDs = []string{"registry", "registry"}
			case "private-environment":
				app.EnvironmentID = "_registries"
			case "missing-authority":
				q.RegistryAuthority = nil
			case "missing-guard":
				q.AdmissionGuard = nil
			case "imported-app":
				app.ID = "vapp_owned"
			}
			if _, err := q.Enqueue(context.Background(), app, "", nil); err == nil || strings.Contains(err.Error(), registryCanary) {
				t.Fatal("unsafe admission")
			}
			if calls != 0 || f.pins != 0 || f.reads != 0 || f.latestReads != 0 {
				t.Fatal("denied authority reached a provider")
			}
			stats, _ := q.Jobs.Stats(context.Background())
			if stats[jobqueue.StatusPending] != 0 {
				t.Fatal("denied admission committed")
			}
		})
	}
}

func TestAppRegistryConcurrentReceiptAndReplacementDoNotRepin(t *testing.T) {
	q, app, f, config := appRegistryQueueFixture(t)
	sha := strings.Repeat("a", 40)
	actor := &model.PipelineRun{StartedByUserSub: "owned-actor", StartedByGroups: []string{"operators"}}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	receipts := make(chan *AppDeployReceipt, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := q.EnqueueIntent(context.Background(), app, sha, actor, "owned-intent")
			errs <- err
			receipts <- r
		}()
	}
	wg.Wait()
	close(errs)
	close(receipts)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first *AppDeployReceipt
	for r := range receipts {
		if first == nil {
			first = r
		} else if *r != *first {
			t.Fatal("intent produced another receipt")
		}
	}
	if f.pins != 1 || f.reads != 0 || f.latestReads != 0 {
		t.Fatal("duplicate admission repinned or read plaintext")
	}
	job, _ := q.Jobs.Get(context.Background(), first.JobID)
	var accepted AppDeployPlan
	if json.Unmarshal(job.Payload, &accepted) != nil || accepted.Registry == nil || accepted.Registry.Connections[config.ID].Credential.Reference.Version != "1" || strings.Contains(string(job.Payload), registryCanary) {
		t.Fatal("accepted authority changed or leaked")
	}
	actor.StartedByUserSub = "changed"
	app.BuildPlan.RegistryConnectionIDs = nil
	f.latest = "2"
	if _, err := q.Store.Registries.(store.RegistryGrantStore).UpdateGrants(context.Background(), config.ID, config.Version, nil, nil); err != nil {
		t.Fatal(err)
	}
	fresh := &AppDeployQueue{Jobs: q.Jobs, RequireRegistryPins: true}
	replayed, err := fresh.EnqueueIntent(context.Background(), app, sha, actor, "owned-intent")
	if err != nil || *replayed != *first || f.pins != 1 {
		t.Fatal("replay adopted replacement authority")
	}
	if accepted.Actor.StartedByUserSub != "owned-actor" {
		t.Fatal("caller actor aliases accepted plan")
	}
	if _, err := fresh.EnqueueIntent(context.Background(), app, strings.Repeat("b", 40), actor, "owned-intent"); err == nil {
		t.Fatal("intent accepted another revision")
	}
}

func TestAppRegistryWorkerRejectsMissingChangedAndWrongEvidence(t *testing.T) {
	for _, name := range []string{"missing", "wrong-subject", "extra-connection", "changed-selection", "revoked", "executor-unconfigured", "previous-run", "previous-stage"} {
		t.Run(name, func(t *testing.T) {
			q, app, f, config := appRegistryQueueFixture(t)
			receipt, err := q.Enqueue(context.Background(), app, strings.Repeat("a", 40), nil)
			if err != nil {
				t.Fatal(err)
			}
			job, _ := q.Jobs.Dequeue(context.Background(), "registry-worker", nil)
			var plan AppDeployPlan
			if err := json.Unmarshal(job.Payload, &plan); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "missing", "previous-run", "previous-stage":
				plan.Registry = nil
			case "wrong-subject":
				plan.Registry.Subject = model.RegistrySubject{Kind: "pipeline", ID: app.ID}
			case "extra-connection":
				pin := plan.Registry.Connections[config.ID]
				pin.ID = "extra"
				plan.Registry.Connections[pin.ID] = pin
			case "changed-selection":
				plan.App.BuildPlan.RegistryConnectionIDs = nil
			case "revoked":
				if _, err := q.Store.Registries.(store.RegistryGrantStore).UpdateGrants(context.Background(), config.ID, config.Version, nil, nil); err != nil {
					t.Fatal(err)
				}
			case "executor-unconfigured":
				q.Deployer.Executor = NewExecutor()
			}
			job.Payload, _ = json.Marshal(plan)
			if name == "previous-run" || name == "previous-stage" {
				run, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
				stamp := time.Now().UTC()
				if name == "previous-run" {
					run.StartedAt = &stamp
				} else {
					run.StageRuns = []model.StageRun{{StageID: "build", StartedAt: &stamp}}
				}
				run.Status = model.RunStatusRunning
				if err := q.Store.Runs.Update(context.Background(), run); err != nil {
					t.Fatal(err)
				}
			}
			clones := 0
			q.Deployer.cloneFn = func(context.Context, github.CloneOptions) (string, error) {
				clones++
				return "", errors.New("rejected registry reached checkout")
			}
			claimed := store.WithRunClaim(context.Background(), job.ID, job.LockedBy, func(ctx context.Context, write func() error) error {
				return q.Jobs.(*jobqueue.Memory).WithClaim(ctx, job.LockedBy, job.ID, write)
			})
			err = q.Handle(claimed, job)
			// Synthetic payload mutations are not the accepted queue input. They must
			// remain uncertain, with zero execution, rather than gain an outcome receipt.
			prior := strings.HasPrefix(name, "previous-") || name == "missing" || name == "wrong-subject" || name == "extra-connection" || name == "changed-selection"
			if errors.Is(err, jobqueue.ErrOutcomeUnknown) != prior {
				t.Fatal("prior effects or fresh preflight misclassified", err)
			}
			run, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
			if !prior && (run.Status != model.RunStatusFailed || !strings.Contains(run.Error, "registry authority")) {
				t.Fatal("missing failure view")
			}
			if clones != 0 || f.reads != 0 || f.latestReads != 0 {
				t.Fatal("rejected worker reached source/credentials")
			}
		})
	}
}

type appRegistryProbe struct{ nativeRegistryProbe }

func (p *appRegistryProbe) Build(ctx context.Context, req builder.Request) (builder.Result, error) {
	p.builds++
	err := p.check(ctx, req.RegistryAuth, req.RegistryHosts, req.LogWriter)
	return builder.Result{ImageID: "sha256:" + strings.Repeat("a", 64), Tags: req.Tags}, err
}
func (p *appRegistryProbe) Push(ctx context.Context, req pusher.Request) (pusher.Result, error) {
	p.pushes++
	err := p.check(ctx, req.RegistryAuth, nil, req.LogWriter)
	return pusher.Result{Digest: "sha256:" + strings.Repeat("a", 64)}, err
}
func (p *appRegistryProbe) Deploy(ctx context.Context, req deployer.Request) (deployer.Result, error) {
	p.deployments++
	if len(req.Env) != 0 {
		p.t.Fatal("registry password entered runtime environment")
	}
	return deployer.Result{}, p.check(ctx, req.RegistryAuth, req.RegistryHosts, req.LogWriter)
}

func ownedAppRegistrySource(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "Dockerfile"}, {"-c", "user.name=Cooker Fixture", "-c", "user.email=cooker@fixture.invalid", "commit", "-qm", "owned source"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("owned Git fixture: %v %s", err, output)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return root, strings.TrimSpace(string(output))
}

func TestAppRegistryUnsupportedTransportRejectsBeforeEffects(t *testing.T) {
	for _, kind := range []model.DeployTargetKind{model.DeployTargetKubernetes, model.DeployTargetECS, model.DeployTargetCloudRun} {
		t.Run(string(kind), func(t *testing.T) {
			q, app, values, _ := appRegistryQueueFixture(t)
			app.DeployTarget.Kind = kind
			calls := 0
			q.ResolveSource = func(context.Context, *model.App, string) (string, error) {
				calls++
				return strings.Repeat("a", 40), nil
			}
			q.PrepareImages = func(context.Context, *AppDeployPlan) (*AppImageResolution, error) {
				calls++
				return nil, errors.New("must not prepare")
			}
			q.Deployer.cloneFn = func(context.Context, github.CloneOptions) (string, error) {
				calls++
				return "", errors.New("must not clone")
			}
			q.Deployer.EnvResolver = &AppEnvResolver{Environments: q.Store.Environments, Secrets: values}
			if _, err := q.Enqueue(context.Background(), app, "", nil); err == nil {
				t.Fatal("unsupported App transport admitted")
			}
			if _, _, err := q.Deployer.Deploy(context.Background(), app, "run", io.Discard); err == nil {
				t.Fatal("unsupported inline transport started")
			}
			if _, _, err := q.Deployer.DeployImage(context.Background(), app, "ghcr.io/team/owned@sha256:"+strings.Repeat("a", 64), "rollback", io.Discard); err == nil {
				t.Fatal("unsupported rollback transport started")
			}
			if calls != 0 || values.pins != 0 || values.reads != 0 || values.latestReads != 0 {
				t.Fatal("known unsupported transport reached source or credentials")
			}
		})
	}
}

func TestAppRegistryStandaloneBuildUsesAppAuthorityBeforeCheckout(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "allowed"}[allowed], func(t *testing.T) {
			q, app, values, _ := appRegistryQueueFixture(t)
			app.DeployTarget.Kind = model.DeployTargetKubernetes
			clones := 0
			if !allowed {
				app.ID = "ungranted"
			}
			probe := &appRegistryProbe{nativeRegistryProbe: nativeRegistryProbe{t: t, expectedUser: "ci"}}
			q.Deployer.Executor = NewExecutor(WithRegistryAuthority(q.RegistryAuthority), WithBuilder(probe), WithPusher(probe))
			q.Deployer.cloneFn = func(context.Context, github.CloneOptions) (string, error) {
				clones++
				values.latest = "2"
				root, _ := ownedAppRegistrySource(t)
				return root, nil
			}
			ref, _, run, err := q.Deployer.BuildAndPushImage(context.Background(), app, "owned-build", io.Discard)
			if !allowed {
				if err == nil || clones != 0 || values.pins != 0 || values.reads != 0 || probe.builds != 0 || probe.pushes != 0 {
					t.Fatal("ungranted standalone build reached source/registry")
				}
				return
			}
			if err != nil || ref == "" || run.Status != model.RunStatusSuccess || clones != 1 || values.pins != 1 || values.reads != 2 || values.latestReads != 0 || probe.deployments != 0 {
				t.Fatal("standalone build lost App authority or original credential", err)
			}
		})
	}
}

func TestAppRegistryCanaryRejectsBeforeReservationAndBuild(t *testing.T) {
	q, app, values, _ := appRegistryQueueFixture(t)
	app.DeployTarget.Kind = model.DeployTargetKubernetes
	app.Canary = model.CanaryConfig{Strategy: model.DeployStrategyCanary, Weight: 20, HealthWindowSeconds: 60}
	weighted := &fakeWeighted{}
	svc := NewCanaryService(q.Store.Apps, q.Store.AppCanaries, q.Store.AppDeploys, q.Deployer, weighted)
	if _, err := svc.Start(context.Background(), app, "owned-canary", io.Discard); err == nil {
		t.Fatal("unsupported canary accepted")
	}
	if _, err := q.Store.AppCanaries.GetActive(context.Background(), app.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("unsupported canary reserved a slot", err)
	}
	if weighted.count() != 0 || values.pins != 0 || values.reads != 0 {
		t.Fatal("unsupported canary reached registry or runtime")
	}
}

func TestAppRegistryDerivedGraphRetainsAppScopeAndOriginalVersion(t *testing.T) {
	for _, selected := range []bool{true, false} {
		t.Run(map[bool]string{true: "selected", false: "anonymous"}[selected], func(t *testing.T) {
			q, app, f, _ := appRegistryQueueFixture(t)
			if !selected {
				app.BuildPlan.RegistryConnectionIDs = nil
			}
			root, sha := ownedAppRegistrySource(t)
			clones := 0
			globalCalls := 0
			user := "ci"
			if !selected {
				user = ""
			}
			probe := &appRegistryProbe{nativeRegistryProbe: nativeRegistryProbe{t: t, expectedUser: user, emit: true}}
			q.Deployer.Executor = NewExecutor(WithRegistryAuthority(q.RegistryAuthority), WithBuilder(probe), WithPusher(probe), WithDockerDeployer(probe), WithRegistryAuth(func(context.Context, string) (authn.Authenticator, error) {
				globalCalls++
				return authn.FromConfig(authn.AuthConfig{Password: "unrelated-global"}), nil
			}))
			q.Deployer.cloneFn = func(context.Context, github.CloneOptions) (string, error) { clones++; return root, nil }
			receipt, err := q.Enqueue(context.Background(), app, sha, &model.PipelineRun{StartedByUserSub: "owned-actor"})
			if err != nil {
				t.Fatal(err)
			}
			f.latest = "2"
			job, _ := q.Jobs.Get(context.Background(), receipt.JobID)
			var logs bytes.Buffer
			q.LogWriter = func(string) io.Writer { return &logs }
			if err := q.Handle(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			run, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
			if run.Status != model.RunStatusSuccess || clones != 1 || probe.builds != 1 || probe.pushes != 1 || probe.deployments != 1 || globalCalls != 0 {
				t.Fatal("derived App graph failed or acquired global authority", run.Status, run.Error)
			}
			serialized, _ := json.Marshal(run)
			if strings.Contains(string(serialized), registryCanary) || strings.Contains(logs.String(), registryCanary) {
				t.Fatal("registry credential leaked")
			}
			wantReads := 0
			if selected {
				wantReads = 3
			}
			if f.reads != wantReads || f.latestReads != 0 {
				t.Fatal("accepted graph adopted latest/unselected credential")
			}
			if err := q.Handle(context.Background(), job); err != nil || clones != 1 {
				t.Fatal("terminal job repeated source/runtime")
			}
		})
	}
}
