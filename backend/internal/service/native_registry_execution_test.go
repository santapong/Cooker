package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/pusher"
	"github.com/santapong/cooker/internal/build/registryauth"
	"github.com/santapong/cooker/internal/build/stagerunner"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func nativeRegistryPipeline() *model.Pipeline {
	p := &model.Pipeline{ID: "pipe-a", Name: "owned registry pipeline", Version: 1, Stages: []model.Stage{{ID: "build", Name: "build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Dockerfile: "Dockerfile", Tags: []string{"ghcr.io/team/app:exact"}, RegistryConnectionIDs: []string{"registry"}}}}}
	p.SetDefaults()
	return p
}

func nativeRegistryEnqueuer(t *testing.T) (*store.Store, *RegistryAuthorityService, *registryVersionFixture, *model.RegistryConfig, *JobQueueEnqueuer, jobqueue.Store, *model.Pipeline) {
	t.Helper()
	st, authority, fixture, config := registryAuthorityFixture(t)
	p := nativeRegistryPipeline()
	if err := st.Pipelines.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewMemory()
	e := NewJobQueueEnqueuer(jobs, st.Pipelines)
	e.AdmissionGuard = memory.NewTriggerStore()
	e.RequireRegistryPins = true
	e.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		return PreparePipelineRegistryAuthority(ctx, authority, p)
	}
	return st, authority, fixture, config, e, jobs, p
}

func acceptedNativeRegistry(t *testing.T, jobs jobqueue.Store, runID string) (*jobqueue.Job, runJobPayload) {
	t.Helper()
	j, err := jobs.Get(context.Background(), "run:"+runID)
	if err != nil {
		t.Fatal(err)
	}
	var payload runJobPayload
	if err = json.Unmarshal(j.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	return j, payload
}

func TestNativeRegistryReceiptFirstConcurrentAdmissionAndRestart(t *testing.T) {
	st, authority, f, _, e, jobs, p := nativeRegistryEnqueuer(t)
	ctx := context.Background()
	run := pendingPipelineRun(p, "owned-registry-restart")
	run.StartedByUserSub = "owned-actor"
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- e.EnqueuePlannedRun(ctx, p, run.Clone()) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	j, accepted := acceptedNativeRegistry(t, jobs, run.ID)
	if f.pins != 1 || accepted.Registry == nil || accepted.Registry.Connections["registry"].Credential.Reference.Version != "1" || strings.Contains(string(j.Payload), registryCanary) {
		t.Fatal("duplicate admission repinned or leaked a credential")
	}
	f.latest = "2"
	fresh := NewJobQueueEnqueuer(jobs, st.Pipelines)
	fresh.AdmissionGuard = memory.NewTriggerStore()
	fresh.RequireRegistryPins = true
	fresh.PrepareRegistry = func(context.Context, *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		t.Fatal("restart repinned accepted authority")
		return nil, nil
	}
	if err := fresh.EnqueuePlannedRun(ctx, p, run.Clone()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := fresh.AcceptedRunSnapshot(ctx, run.ID)
	if err != nil || snapshot.StartedByUserSub != run.StartedByUserSub {
		t.Fatal("accepted actor snapshot unavailable")
	}
	snapshot.StartedByUserSub = "mutated"
	if again, _ := fresh.AcceptedRunSnapshot(ctx, run.ID); again.StartedByUserSub != run.StartedByUserSub {
		t.Fatal("snapshot aliases admission")
	}
	b := &nativeRegistryProbe{t: t, expectedUser: "ci"}
	exec := NewExecutor(WithBuilder(b), WithRegistryAuthority(authority))
	runner := NewJobQueueRunner(st, exec, nil)
	runner.RegistryAuthority = authority
	if err = runner.Handle(ctx, j); err != nil {
		t.Fatal(err)
	}
	stored, err := st.Runs.Get(ctx, run.ID)
	if err != nil || stored.Status != model.RunStatusSuccess || b.builds != 1 || f.reads != 1 || f.latestReads != 0 {
		t.Fatal("fresh worker did not consume accepted version")
	}
	if err = runner.Handle(ctx, j); err != nil || b.builds != 1 {
		t.Fatal("terminal job repeated adapter work")
	}
	changed := *p
	changed.Version++
	if err = fresh.EnqueuePlannedRun(ctx, &changed, pendingPipelineRun(&changed, run.ID)); err == nil {
		t.Fatal("accepted run changed pipeline")
	}
}

func TestNativeRegistryAdmissionRejectsBeforeProviderSourceAndRuntimeSecrets(t *testing.T) {
	for _, name := range []string{"ungranted", "private-runtime", "missing-pinner", "missing-guard", "malformed-result"} {
		t.Run(name, func(t *testing.T) {
			_, _, f, _, e, jobs, p := nativeRegistryEnqueuer(t)
			calls := 0
			e.PrepareSource = func(context.Context, *model.Pipeline) (*model.PipelineSourcePlan, error) {
				calls++
				return nil, errors.New("source reached")
			}
			e.PrepareSecrets = func(context.Context, *model.Pipeline) (*PipelineSecretPlan, error) {
				calls++
				return nil, errors.New("runtime secret reached")
			}
			switch name {
			case "ungranted":
				p.ID = "pipe-b"
			case "private-runtime":
				p.Stages[0].EnvironmentID = "registry_6a2fc7f1e32640f5ab7d0b5f9400d663"
			case "missing-pinner":
				e.PrepareRegistry = nil
			case "missing-guard":
				e.AdmissionGuard = nil
			case "malformed-result":
				e.PrepareRegistry = func(context.Context, *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
					return &model.RegistryAuthorityPlan{}, nil
				}
			}
			run := pendingPipelineRun(p, "owned-registry-denied")
			if err := e.EnqueuePlannedRun(context.Background(), p, run); err == nil || strings.Contains(err.Error(), registryCanary) {
				t.Fatal("unsafe admission accepted")
			}
			if calls != 0 || f.pins != 0 || f.reads != 0 || f.latestReads != 0 {
				t.Fatal("rejected admission reached provider/runtime preparation")
			}
			if _, err := jobs.Get(context.Background(), "run:"+run.ID); !errors.Is(err, jobqueue.ErrNotFound) {
				t.Fatal("denied admission committed")
			}
		})
	}
}

type nativeRegistryAtomicQueue struct{ jobqueue.Store }

func (nativeRegistryAtomicQueue) AtomicTriggerAdmission() bool { return true }

func TestNativeRegistryScheduledAdmissionPinsAndRecoversOriginalOccurrence(t *testing.T) {
	st, authority, f, _, _, jobs, p := nativeRegistryEnqueuer(t)
	e := NewJobQueueEnqueuer(nativeRegistryAtomicQueue{jobs}, st.Pipelines)
	e.AdmissionGuard = memory.NewTriggerStore()
	e.RequireRegistryPins = true
	e.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		return PreparePipelineRegistryAuthority(ctx, authority, p)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	o := ScheduledOccurrence{ScheduleID: "owned-schedule", ScheduleVersion: 1, ScheduledFor: now, AdmittedAt: now, NextRunAt: now.Add(time.Hour), PipelineID: p.ID, CronExpr: "0 * * * *", Timezone: "UTC"}
	if err := e.EnqueueScheduledRun(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	_, accepted := acceptedNativeRegistry(t, jobs, o.RunID())
	f.latest = "2"
	e.PrepareRegistry = func(context.Context, *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		t.Fatal("scheduled restart repinned")
		return nil, nil
	}
	if err := e.EnqueueScheduledRun(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if accepted.Registry == nil || f.pins != 1 {
		t.Fatal("scheduled payload omitted pins")
	}
}

func TestNativeRegistrySourceReceiptSnapshotAndTriggerPayload(t *testing.T) {
	s, p, sourceFixture := nativeSourceSetup(t)
	_, _, f, r := registryAuthorityFixture(t)
	r.AllowedPipelineIDs = []string{p.ID}
	if err := s.Store.Registries.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	authority := NewRegistryAuthorityService(s.Store, f)
	p.Stages[0].Config.RegistryConnectionIDs = []string{r.ID}
	jobs := jobqueue.NewMemory()
	e := NewJobQueueEnqueuer(nativeRegistryAtomicQueue{jobs}, s.Store.Pipelines)
	e.AdmissionGuard = memory.NewTriggerStore()
	e.RequireRegistryPins = true
	e.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		return PreparePipelineRegistryAuthority(ctx, authority, p)
	}
	e.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
		return s.Prepare(ctx, p, p.Source.FullRef, "manual")
	}
	run := pendingPipelineRun(p, "owned-source-registry")
	if err := e.EnqueuePlannedRun(context.Background(), p, run); err != nil {
		t.Fatal(err)
	}
	if run.Source != nil {
		t.Fatal("admission mutated caller run")
	}
	snapshot, err := e.AcceptedRunSnapshot(context.Background(), run.ID)
	if err != nil || snapshot.Source == nil || snapshot.Source.CheckedOutAt != nil || snapshot.Source.CommitSHA != strings.Repeat("a", 40) {
		t.Fatal("public accepted source is not materialized truthfully")
	}
	before := sourceFixture.checkouts
	if _, err = e.AcceptedRunSnapshot(context.Background(), run.ID); err != nil || sourceFixture.checkouts != before {
		t.Fatal("snapshot checked out or reselected source")
	}
	source, err := s.Prepare(context.Background(), p, p.Source.FullRef, "manual")
	if err != nil {
		t.Fatal(err)
	}
	source.ReviewPolicy = "automatic"
	source.PlanHash = pipelineSourcePlanHash(p, source)
	digest, err := model.PipelineSourcePlanDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	proof := &model.PipelineTriggerProof{IntentID: "owned-intent", RuleID: "owned-rule", RuleVersion: 1, TargetKind: "pipeline", TargetID: p.ID, SourcePlanDigest: digest, EnvelopeHash: source.EnvelopeHash}
	autoRun := pendingPipelineRun(p, "owned-trigger-registry")
	if _, err = e.EnqueueSourceRun(context.Background(), p, autoRun, source, proof); err != nil {
		t.Fatal(err)
	}
	_, accepted := acceptedNativeRegistry(t, jobs, autoRun.ID)
	if accepted.Registry == nil || accepted.Trigger == nil {
		t.Fatal("trigger payload omitted registry authority")
	}
}

func TestNativeRegistryWorkerRejectsTamperingBeforeSourceOrAdapters(t *testing.T) {
	for _, name := range []string{"missing", "wrong-subject", "extra-stage", "changed-selection", "revoked", "private-runtime", "previous-attempt"} {
		t.Run(name, func(t *testing.T) {
			st, authority, f, config, e, jobs, p := nativeRegistryEnqueuer(t)
			run := pendingPipelineRun(p, "owned-worker-registry-denied")
			if err := e.EnqueuePlannedRun(context.Background(), p, run); err != nil {
				t.Fatal(err)
			}
			j, payload := acceptedNativeRegistry(t, jobs, run.ID)
			switch name {
			case "missing", "previous-attempt":
				payload.Registry = nil
			case "wrong-subject":
				payload.Registry.Subject = model.RegistrySubject{Kind: "app", ID: "app-a"}
			case "extra-stage":
				payload.Registry.Stages["injected"] = nil
			case "changed-selection":
				payload.Pipeline.Stages[0].Config.RegistryConnectionIDs = nil
			case "private-runtime":
				payload.Pipeline.Stages[0].EnvironmentID = "_registries"
			case "revoked":
				if _, err := st.Registries.(store.RegistryGrantStore).UpdateGrants(context.Background(), config.ID, config.Version, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			j.Payload, _ = json.Marshal(payload)
			if name == "previous-attempt" {
				old := run.Clone()
				stamp := time.Now()
				old.StartedAt = &stamp
				old.Status = model.RunStatusRunning
				if err := st.Runs.Create(context.Background(), old); err != nil {
					t.Fatal(err)
				}
			}
			probe := &nativeRegistryProbe{t: t}
			runner := NewJobQueueRunner(st, NewExecutor(WithBuilder(probe), WithRegistryAuthority(authority)), nil)
			runner.RegistryAuthority = authority
			err := runner.Handle(context.Background(), j)
			if err == nil || probe.builds != 0 || f.reads != 0 || f.latestReads != 0 {
				t.Fatal("rejected worker reached credential or adapter")
			}
			if errors.Is(err, jobqueue.ErrOutcomeUnknown) != (name == "previous-attempt") {
				t.Fatal("fresh rejection or previous-attempt uncertainty misreported")
			}
		})
	}
}

type nativeRegistryProbe struct {
	t                                       *testing.T
	expectedUser                            string
	builds, pushes, containers, deployments int
	emit, fail                              bool
}

func (p *nativeRegistryProbe) check(ctx context.Context, resolve registryauth.Resolver, hosts []string, writer io.Writer) error {
	p.t.Helper()
	if resolve == nil {
		p.t.Fatal("accepted adapter lacks explicit resolver")
	}
	a, err := resolve(ctx, "ghcr.io")
	if err != nil {
		return err
	}
	cfg, err := a.Authorization()
	if err != nil {
		return err
	}
	if cfg.Username != p.expectedUser {
		p.t.Fatalf("stage received user %q, expected %q", cfg.Username, p.expectedUser)
	}
	other, err := resolve(ctx, "unrelated.invalid")
	if err != nil {
		return err
	}
	unrelated, _ := other.Authorization()
	if unrelated.Password != "" {
		p.t.Fatal("unselected host borrowed authority")
	}
	if p.emit && cfg.Password != "" {
		forms := []string{cfg.Password, cfg.Username + ":" + cfg.Password, base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password)), url.QueryEscape(cfg.Password)}
		for _, form := range forms {
			fmt.Fprintln(writer, "owned adapter output", form)
		}
		if p.fail {
			return errors.New("owned adapter error " + cfg.Password)
		}
	}
	return nil
}
func (p *nativeRegistryProbe) Build(ctx context.Context, req builder.Request) (builder.Result, error) {
	p.builds++
	err := p.check(ctx, req.RegistryAuth, req.RegistryHosts, req.LogWriter)
	return builder.Result{ImageID: "sha256:owned", Tags: req.Tags}, err
}
func (p *nativeRegistryProbe) Push(ctx context.Context, req pusher.Request) (pusher.Result, error) {
	p.pushes++
	err := p.check(ctx, req.RegistryAuth, nil, req.LogWriter)
	return pusher.Result{Digest: "sha256:owned"}, err
}
func (p *nativeRegistryProbe) Run(ctx context.Context, req stagerunner.Request) (stagerunner.Result, error) {
	p.containers++
	err := p.check(ctx, req.RegistryAuth, req.RegistryHosts, req.LogWriter)
	if len(req.Env) != 0 {
		p.t.Fatal("registry credential became runtime environment")
	}
	return stagerunner.Result{}, err
}
func (p *nativeRegistryProbe) Deploy(ctx context.Context, req deployer.Request) (deployer.Result, error) {
	p.deployments++
	return deployer.Result{}, p.check(ctx, req.RegistryAuth, req.RegistryHosts, req.LogWriter)
}

func TestNativeRegistryEveryStageScopedAndAnonymousCannotBorrowGlobal(t *testing.T) {
	for _, kind := range []struct {
		stageType model.StageType
		runtime   string
	}{{model.StageTypeBuild, ""}, {model.StageTypePush, ""}, {model.StageTypeTest, ""}, {model.StageTypeCustom, ""}, {model.StageTypeDeploy, "docker"}, {model.StageTypeDeploy, "compose"}} {
		stageType := kind.stageType
		for _, selected := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s-%s-selected-%v", stageType, kind.runtime, selected), func(t *testing.T) {
				_, authority, f, _, _, _, p := nativeRegistryEnqueuer(t)
				stage := &p.Stages[0]
				stage.Type = stageType
				stage.Config.Image = "ghcr.io/team/app:exact"
				stage.Config.Repository = "ghcr.io/team/app:exact"
				stage.Config.Script = "echo owned"
				stage.Config.DeployRuntime = kind.runtime
				stage.Config.RuntimeName = "owned-app"
				user := "ci"
				if !selected {
					stage.Config.RegistryConnectionIDs = nil
					user = ""
				}
				plan, err := PreparePipelineRegistryAuthority(context.Background(), authority, p)
				if err != nil {
					t.Fatal(err)
				}
				globalCalls := 0
				probe := &nativeRegistryProbe{t: t, expectedUser: user, emit: true}
				exec := NewExecutor(WithRegistryAuthority(authority), WithBuilder(probe), WithPusher(probe), WithStageRunner(probe), WithDockerDeployer(probe), WithComposeDeployer(probe), WithRegistryAuth(func(context.Context, string) (authn.Authenticator, error) {
					globalCalls++
					return authn.FromConfig(authn.AuthConfig{Password: "unrelated-global"}), nil
				}))
				ctx := WithAcceptedRegistryAuthority(context.Background(), model.RegistrySubject{Kind: "pipeline", ID: p.ID}, plan)
				run := pendingPipelineRun(p, "owned-stage-scope")
				result, err := exec.Execute(ctx, p, run)
				if err != nil || result.Status != model.RunStatusSuccess {
					t.Fatalf("stage failed: %v", err)
				}
				if globalCalls != 0 || f.latestReads != 0 || !selected && f.reads != 0 {
					t.Fatal("accepted anonymous plan borrowed global or latest credentials")
				}
				public, _ := json.Marshal(run)
				for _, canary := range []string{registryCanary, base64.StdEncoding.EncodeToString([]byte("ci:" + registryCanary))} {
					if strings.Contains(string(public), canary) {
						t.Fatal("credential leaked into persisted run")
					}
				}
			})
		}
	}
}

func TestNativeRegistryAdapterErrorRedactedAndAppSubjectPreserved(t *testing.T) {
	_, authority, _, _, _, _, p := nativeRegistryEnqueuer(t)
	plan, err := authority.Prepare(context.Background(), model.RegistrySubject{Kind: "app", ID: "app-a"}, []string{"registry"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	probe := &nativeRegistryProbe{t: t, expectedUser: "ci", emit: true, fail: true}
	exec := NewExecutor(WithRegistryAuthority(authority), WithBuilder(probe))
	ctx := WithAcceptedRegistryAuthority(context.Background(), model.RegistrySubject{Kind: "app", ID: "app-a"}, plan)
	run := pendingPipelineRun(p, "owned-app-subject")
	_, err = exec.Execute(ctx, p, run)
	public, _ := json.Marshal(run)
	if err == nil || probe.builds != 1 || strings.Contains(string(public), registryCanary) || strings.Contains(err.Error(), registryCanary) {
		t.Fatal("app subject/error redaction boundary failed")
	}
}

type nativeRegistryEnvironmentProbe struct {
	store.EnvironmentStore
	gets int
}

func (p *nativeRegistryEnvironmentProbe) Get(ctx context.Context, id string) (*model.Environment, error) {
	p.gets++
	return p.EnvironmentStore.Get(ctx, id)
}

func nativeUnsupportedDeployment(runtime string) model.Stage {
	return model.Stage{ID: "deploy", Type: model.StageTypeDeploy, EnvironmentID: "owned-runtime", Config: model.StageConfig{DeployRuntime: runtime, ManifestPath: "owned.yaml", HelmChart: "owned-chart"}}
}

func TestNativeRegistryUnsupportedDeploymentPreparationReadsNothing(t *testing.T) {
	for runtimeIndex, runtime := range []string{"", "kubernetes", "helm", "ecs", "cloud-run", "docker ", registryCanary} {
		for _, selected := range []bool{true, false} {
			t.Run(fmt.Sprintf("runtime-%d-selected-%v", runtimeIndex, selected), func(t *testing.T) {
				st, authority, f, _, _, _, p := nativeRegistryEnqueuer(t)
				if !selected {
					p.Stages[0].Config.RegistryConnectionIDs = nil
				}
				p.Stages = append(p.Stages, nativeUnsupportedDeployment(runtime))
				envs := &nativeRegistryEnvironmentProbe{EnvironmentStore: st.Environments}
				authority.Environments = envs
				plan, err := PreparePipelineRegistryAuthority(context.Background(), authority, p)
				if !errors.Is(err, deployer.ErrUnavailable) || plan != nil || strings.Contains(err.Error(), registryCanary) || envs.gets != 0 || f.pins != 0 || f.reads != 0 || f.latestReads != 0 {
					t.Fatal("unsupported transport reached metadata or secret preparation")
				}
			})
		}
	}
}

func TestNativeRegistryUnsupportedDeploymentAdmissionSkipsAllPreparers(t *testing.T) {
	for _, path := range []string{"manual", "scheduled", "trigger"} {
		for _, runtime := range []string{"kubernetes", "ecs", "cloud-run"} {
			for _, selected := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s-%s-selected-%v", path, runtime, selected), func(t *testing.T) {
					s, p, sourceFixture := nativeSourceSetup(t)
					if selected {
						p.Stages[0].Config.RegistryConnectionIDs = []string{"registry"}
					}
					p.Stages = append(p.Stages, nativeUnsupportedDeployment(runtime))
					if err := s.Store.Pipelines.Update(context.Background(), p); err != nil {
						t.Fatal(err)
					}
					jobs := jobqueue.NewMemory()
					e := NewJobQueueEnqueuer(nativeRegistryAtomicQueue{jobs}, s.Store.Pipelines)
					e.AdmissionGuard = memory.NewTriggerStore()
					e.RequireRegistryPins = true
					registryCalls, sourceCalls, secretCalls := 0, 0, 0
					e.PrepareRegistry = func(context.Context, *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
						registryCalls++
						return nil, errors.New(registryCanary)
					}
					e.PrepareSource = func(context.Context, *model.Pipeline) (*model.PipelineSourcePlan, error) {
						sourceCalls++
						return nil, errors.New(registryCanary)
					}
					e.PrepareSecrets = func(context.Context, *model.Pipeline) (*PipelineSecretPlan, error) {
						secretCalls++
						return nil, errors.New(registryCanary)
					}
					run := pendingPipelineRun(p, "owned-unsupported-admission")
					var err error
					switch path {
					case "manual":
						err = e.EnqueuePlannedRun(context.Background(), p, run)
					case "scheduled":
						now := time.Now().UTC().Truncate(time.Microsecond)
						o := ScheduledOccurrence{ScheduleID: "owned-schedule", ScheduleVersion: 1, ScheduledFor: now, AdmittedAt: now, NextRunAt: now.Add(time.Hour), PipelineID: p.ID, CronExpr: "0 * * * *", Timezone: "UTC"}
						run.ID = o.RunID()
						err = e.EnqueueScheduledRun(context.Background(), o)
					case "trigger":
						_, err = e.EnqueueSourceRun(context.Background(), p, run, &model.PipelineSourcePlan{}, &model.PipelineTriggerProof{})
					}
					if !errors.Is(err, deployer.ErrUnavailable) || strings.Contains(err.Error(), registryCanary) || registryCalls != 0 || sourceCalls != 0 || secretCalls != 0 || sourceFixture.identities != 0 || sourceFixture.revisions != 0 || sourceFixture.checkouts != 0 {
						t.Fatal("unsupported admission reached a preparer")
					}
					if _, err := jobs.Get(context.Background(), "run:"+run.ID); !errors.Is(err, jobqueue.ErrNotFound) {
						t.Fatal("unsupported admission committed a job")
					}
				})
			}
		}
	}
}

func TestNativeRegistryUnsupportedAcceptedTransportRejectsBeforeCheckoutAndStages(t *testing.T) {
	for _, runtime := range []string{"", "kubernetes", "helm", "ecs", "cloud-run"} {
		for _, selected := range []bool{true, false} {
			for _, previousAttempt := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s-selected-%v-previous-%v", runtime, selected, previousAttempt), func(t *testing.T) {
					s, p, sourceFixture := nativeSourceSetup(t)
					configuredSource := p.Source
					// Source-bound native plans already reject non-Docker deployment
					// definitions. Exercise the independently accepted host-context
					// build path that previously reached stages before the guard.
					p.Source = nil
					if err := s.Store.Environments.Create(context.Background(), &model.Environment{ID: "owned-runtime", Name: "owned runtime"}); err != nil {
						t.Fatal(err)
					}
					_, _, f, config := registryAuthorityFixture(t)
					config.AllowedPipelineIDs = []string{p.ID}
					if err := s.Store.Registries.Create(context.Background(), config); err != nil {
						t.Fatal(err)
					}
					if selected {
						p.Stages[0].Config.RegistryConnectionIDs = []string{config.ID}
					}
					p.Stages = append(p.Stages, nativeUnsupportedDeployment(runtime))
					authority := NewRegistryAuthorityService(s.Store, f)
					// Model an older accepted job that predates transport preflight.
					// Construct exact authority directly, never through the repaired
					// native preparation API that must now reject this pipeline.
					ids, bindings, err := PipelineRegistrySelection(p)
					if err != nil {
						t.Fatal(err)
					}
					plan, err := authority.Prepare(context.Background(), model.RegistrySubject{Kind: "pipeline", ID: p.ID}, ids, bindings)
					if err != nil {
						t.Fatal(err)
					}
					run := pendingPipelineRun(p, "owned-unsupported-accepted")
					payload, err := json.Marshal(runJobPayload{PipelineID: p.ID, RunID: run.ID, Pipeline: p, Run: run, Registry: plan})
					if err != nil {
						t.Fatal(err)
					}
					jobs := jobqueue.NewMemory()
					j, err := jobs.Enqueue(context.Background(), JobKindPipelineRun, payload, jobqueue.EnqueueOptions{ID: "run:" + run.ID, MaxAttempts: 1, ConcurrencyKey: "pipeline:" + p.ID})
					if err != nil {
						t.Fatal(err)
					}
					e := NewJobQueueEnqueuer(jobs, s.Store.Pipelines)
					e.AdmissionGuard = memory.NewTriggerStore()
					e.RequireRegistryPins = true
					e.PrepareRegistry = func(context.Context, *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
						t.Fatal("accepted receipt was repinned")
						return nil, nil
					}
					if err = e.EnqueuePlannedRun(context.Background(), p, run); err != nil {
						t.Fatal("original receipt recovery failed", err)
					}
					if snapshot, err := e.AcceptedRunSnapshot(context.Background(), run.ID); err != nil || snapshot.Source != nil {
						t.Fatal("accepted receipt was lost or claimed checkout")
					}
					if previousAttempt {
						old := run.Clone()
						at := time.Now()
						old.StartedAt, old.Status = &at, model.RunStatusRunning
						if err := s.Store.Runs.Create(context.Background(), old); err != nil {
							t.Fatal(err)
						}
					}
					envs := &nativeRegistryEnvironmentProbe{EnvironmentStore: s.Store.Environments}
					authority.Environments = envs
					user := ""
					if selected {
						user = "ci"
					}
					probe := &nativeRegistryProbe{t: t, expectedUser: user}
					exec := NewExecutor(WithRegistryAuthority(authority), WithEnvironmentResolver(&AppEnvResolver{Environments: envs, Secrets: f}), WithBuilder(probe), WithPusher(probe), WithStageRunner(probe), WithDeployer(probe))
					runner := NewJobQueueRunner(s.Store, exec, nil)
					runner.SourcePreparer = s // executor-only authority exercises the previous guard fix
					beforePins, beforeReads, beforeCheckouts := f.pins, f.reads, sourceFixture.checkouts
					err = runner.Handle(context.Background(), j)
					if err == nil || !previousAttempt && !errors.Is(err, deployer.ErrUnavailable) || errors.Is(err, jobqueue.ErrOutcomeUnknown) != previousAttempt || f.pins != beforePins || f.reads != beforeReads || f.latestReads != 0 || sourceFixture.checkouts != beforeCheckouts || envs.gets != 0 || probe.builds != 0 || probe.pushes != 0 || probe.containers != 0 || probe.deployments != 0 {
						t.Fatal("unsupported accepted transport reached checkout, metadata, secrets or stages")
					}
					stored, err := s.Store.Runs.Get(context.Background(), run.ID)
					if err != nil || stored.Source != nil || stored.Error != deployer.ErrUnavailable.Error()+": accepted registry transport is unavailable for this deployment runtime" {
						t.Fatal("rejected worker lost the capability error or claimed checkout")
					}
					for _, stage := range stored.StageRuns {
						if stage.StartedAt != nil {
							t.Fatal("rejected worker started a stage")
						}
					}
					// An accepted native Executor also rejects the unsupported
					// transport before requiring or opening source context.
					p.Source = configuredSource
					ctx := WithAcceptedRegistryAuthority(context.Background(), model.RegistrySubject{Kind: "pipeline", ID: p.ID}, plan)
					_, err = exec.Execute(ctx, p, pendingPipelineRun(p, "owned-unsupported-inline"))
					var preflight *pipelinePreflightError
					if !errors.Is(err, deployer.ErrUnavailable) || !errors.As(err, &preflight) || envs.gets != 0 || probe.builds != 0 || f.reads != beforeReads {
						t.Fatal("accepted executor did not reject before DAG/runtime resolution")
					}
				})
			}
		}
	}
}

func TestNativeRegistryStageUnionAndSameHostCredentialsStaySeparate(t *testing.T) {
	st, authority, f, original, _, _, p := nativeRegistryEnqueuer(t)
	second := original.Clone()
	second.ID, second.Name, second.Username = "second-registry", "second registry", "second-ci"
	second.PasswordRef = "registry:v2:83267112-eded-4a70-9c3d-c48a67d4b6b9"
	if err := st.Registries.Create(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	p.Stages = append(p.Stages, model.Stage{ID: "second", Type: model.StageTypeTest, Config: model.StageConfig{Image: "ghcr.io/owned/second", RegistryConnectionIDs: []string{second.ID}}}, model.Stage{ID: "public", Type: model.StageTypeTest, Config: model.StageConfig{Image: "ghcr.io/owned/public"}})
	plan, err := PreparePipelineRegistryAuthority(context.Background(), authority, p)
	if err != nil || len(plan.Connections) != 2 || len(plan.Stages) != 3 {
		t.Fatal("exact authored stage union was not captured")
	}
	ctx := WithAcceptedRegistryAuthority(WithLogSecrets(context.Background(), nil), model.RegistrySubject{Kind: "pipeline", ID: p.ID}, plan)
	exec := NewExecutor(WithRegistryAuthority(authority))
	for _, item := range []struct {
		stage int
		user  string
		hosts int
	}{{0, "ci", 1}, {1, "second-ci", 1}, {2, "", 0}} {
		resolve, hosts, err := exec.stageRegistry(ctx, &p.Stages[item.stage])
		if err != nil || len(hosts) != item.hosts {
			t.Fatal("stage host scope mismatch")
		}
		a, err := resolve(ctx, "ghcr.io")
		if err != nil {
			t.Fatal(err)
		}
		config, _ := a.Authorization()
		if config.Username != item.user {
			t.Fatal("stage borrowed another same-host credential")
		}
	}
	if f.reads != 2 || f.latestReads != 0 {
		t.Fatal("anonymous stage queried secret provider")
	}
	for _, mutation := range []string{"remove-stage", "duplicate-stage", "duplicate-selection", "union-injection", "imported-id"} {
		t.Run(mutation, func(t *testing.T) {
			data, _ := json.Marshal(p)
			var changed model.Pipeline
			_ = json.Unmarshal(data, &changed)
			switch mutation {
			case "remove-stage":
				changed.Stages = changed.Stages[:2]
			case "duplicate-stage":
				changed.Stages[2].ID = changed.Stages[0].ID
			case "duplicate-selection":
				changed.Stages[0].Config.RegistryConnectionIDs = []string{"registry", "registry"}
			case "union-injection":
				changed.Stages[2].Config.RegistryConnectionIDs = []string{"registry"}
			case "imported-id":
				changed.ID = "vflow_imported"
			}
			if validateNativeRegistryPlan(&changed, plan) == nil {
				t.Fatal("changed stage selection accepted")
			}
		})
	}
}

func TestNativeRegistryRevocationRejectsBeforeAcceptedSourceCheckout(t *testing.T) {
	s, p, sourceFixture := nativeSourceSetup(t)
	_, _, f, r := registryAuthorityFixture(t)
	r.AllowedPipelineIDs = []string{p.ID}
	if err := s.Store.Registries.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	authority := NewRegistryAuthorityService(s.Store, f)
	p.Stages[0].Config.RegistryConnectionIDs = []string{r.ID}
	jobs := jobqueue.NewMemory()
	e := NewJobQueueEnqueuer(jobs, s.Store.Pipelines)
	e.AdmissionGuard = memory.NewTriggerStore()
	e.RequireRegistryPins = true
	e.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		return PreparePipelineRegistryAuthority(ctx, authority, p)
	}
	e.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
		return s.Prepare(ctx, p, p.Source.FullRef, "manual")
	}
	run := pendingPipelineRun(p, "owned-source-before-registry")
	if err := e.EnqueuePlannedRun(context.Background(), p, run); err != nil {
		t.Fatal(err)
	}
	j, _ := acceptedNativeRegistry(t, jobs, run.ID)
	if _, err := s.Store.Registries.(store.RegistryGrantStore).UpdateGrants(context.Background(), r.ID, r.Version, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := sourceFixture.checkouts
	probe := &nativeRegistryProbe{t: t}
	runner := NewJobQueueRunner(s.Store, NewExecutor(WithRegistryAuthority(authority), WithBuilder(probe)), nil)
	runner.RegistryAuthority = authority
	runner.SourcePreparer = s
	if err := runner.Handle(context.Background(), j); err == nil || errors.Is(err, jobqueue.ErrOutcomeUnknown) {
		t.Fatal("fresh registry rejection was not determinate")
	}
	stored, err := s.Store.Runs.Get(context.Background(), run.ID)
	if err != nil || stored.Source == nil || stored.Source.CheckedOutAt != nil || sourceFixture.checkouts != before || f.reads != 0 || probe.builds != 0 {
		t.Fatal("revoked registry authority reached accepted source checkout")
	}
}

func TestNativeRegistryExecutorOnlyWorkerRejectsMissingEvidenceWithoutRepinning(t *testing.T) {
	for _, sourceBound := range []bool{false, true} {
		for _, previousAttempt := range []bool{false, true} {
			t.Run(fmt.Sprintf("source-%v-prior-%v", sourceBound, previousAttempt), func(t *testing.T) {
				st, authority, f, _, e, jobs, p := nativeRegistryEnqueuer(t)
				var source *PipelineSourcePreparer
				var sourceFixture *nativeSourceFixture
				if sourceBound {
					source, p, sourceFixture = nativeSourceSetup(t)
					var config *model.RegistryConfig
					_, _, f, config = registryAuthorityFixture(t)
					config.AllowedPipelineIDs = []string{p.ID}
					if err := source.Store.Registries.Create(context.Background(), config); err != nil {
						t.Fatal(err)
					}
					st = source.Store
					authority = NewRegistryAuthorityService(st, f)
					p.Stages[0].Config.RegistryConnectionIDs = []string{config.ID}
					e = NewJobQueueEnqueuer(jobs, st.Pipelines)
					e.RequireRegistryPins = true
					e.AdmissionGuard = memory.NewTriggerStore()
					e.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
						return PreparePipelineRegistryAuthority(ctx, authority, p)
					}
					e.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
						return source.Prepare(ctx, p, p.Source.FullRef, "manual")
					}
				}
				run := pendingPipelineRun(p, "owned-missing-worker-evidence")
				if err := e.EnqueuePlannedRun(context.Background(), p, run); err != nil {
					t.Fatal(err)
				}
				j, payload := acceptedNativeRegistry(t, jobs, run.ID)
				payload.Registry = nil
				j.Payload, _ = json.Marshal(payload)
				// A genuinely restarted process has only durable input; the
				// executor option must still require accepted registry evidence.
				probe := &nativeRegistryProbe{t: t}
				runner := NewJobQueueRunner(st, NewExecutor(WithRegistryAuthority(authority), WithBuilder(probe)), nil)
				runner.SourcePreparer = source
				beforePins, beforeReads, beforeCheckouts := f.pins, f.reads, 0
				if sourceFixture != nil {
					beforeCheckouts = sourceFixture.checkouts
				}
				if previousAttempt {
					old := run.Clone()
					at := time.Now()
					old.StartedAt = &at
					old.Status = model.RunStatusRunning
					if err := st.Runs.Create(context.Background(), old); err != nil {
						t.Fatal(err)
					}
				}
				err := runner.Handle(context.Background(), j)
				if err == nil || errors.Is(err, jobqueue.ErrOutcomeUnknown) != previousAttempt || f.pins != beforePins || f.reads != beforeReads || f.latestReads != 0 || probe.builds != 0 || sourceFixture != nil && sourceFixture.checkouts != beforeCheckouts {
					t.Fatal("missing durable evidence triggered fresh preparation or stage effects")
				}
				stored, err := st.Runs.Get(context.Background(), run.ID)
				if err != nil || stored.Source != nil && stored.Source.CheckedOutAt != nil {
					t.Fatal("recovery claimed source checkout")
				}
			})
		}
	}
}
