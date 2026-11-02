package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store/memory"
)

// Only Get is available in this read-only unit fixture. Any attempted queue
// execution/mutation hits the nil embedded interface instead of faking a worker.
type nativeRuntimeJobs struct {
	jobqueue.Store
	job *jobqueue.Job
	err error
}

func (s *nativeRuntimeJobs) Get(_ context.Context, id string) (*jobqueue.Job, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.job == nil || s.job.ID != id {
		return nil, jobqueue.ErrNotFound
	}
	copy := *s.job
	copy.Payload = append([]byte{}, s.job.Payload...)
	return &copy, nil
}

type nativeRuntimeFixture struct {
	r          *RuntimeService
	p          *model.Pipeline
	run        *model.PipelineRun
	planned    *model.PipelineRun
	payload    runJobPayload
	jobs       *nativeRuntimeJobs
	dir, calls string
}

func newNativeRuntimeFixture(t *testing.T) *nativeRuntimeFixture {
	t.Helper()
	p := &model.Pipeline{ID: "native-pipeline", Name: "native", Version: 1,
		Stages: []model.Stage{*receiptStage("deploy-web", "web")}}
	p.SetDefaults()
	r := &RuntimeService{Store: memory.New()}
	if err := r.Store.Pipelines.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f := &nativeRuntimeFixture{r: r, p: p, jobs: &nativeRuntimeJobs{}, dir: t.TempDir()}
	f.resetRun()
	f.calls = filepath.Join(f.dir, "calls")
	r.DockerBin = filepath.Join(f.dir, "docker-native")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$COOKER_NATIVE_DIR/calls"
[ "$1" = container ] && [ "$2" = inspect ] && [ "$3" = --format ] || exit 91
for target; do :; done
if [ "$COOKER_NATIVE_MODE" = fail ]; then printf '%s' private-native-stderr-canary >&2; exit 44; fi
if [ "$COOKER_NATIVE_MODE" = wait ]; then exec sleep 60; fi
if [ "$COOKER_NATIVE_MODE" = descendant ]; then
 (count=0; while [ ! -f "$COOKER_NATIVE_DIR/release" ] && [ "$count" -lt 100 ]; do sleep 0.02; count=$((count + 1)); done; printf '%s' done > "$COOKER_NATIVE_DIR/done") &
fi
cat "$COOKER_NATIVE_DIR/$target.json"
`
	if err := os.WriteFile(r.DockerBin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COOKER_NATIVE_DIR", f.dir)
	t.Setenv("COOKER_NATIVE_MODE", "")
	f.response(t, f.run.StageRuns[0].RuntimeReceipt, "running", "configured", "healthy")
	return f
}

func (f *nativeRuntimeFixture) resetRun() {
	f.planned = pendingPipelineRun(f.p, "native-run")
	f.planned.StartedByUserSub = "operator"
	f.planned.StartedByEmail = "operator@example.invalid"
	f.planned.StartedByGroups = []string{"operators"}
	f.planned.StartedByTokenHash = "accepted-token-hash"
	f.run = f.planned.Clone()
	f.run.Status = model.RunStatusSuccess
	started, finished := time.Now().Add(-2*time.Second), time.Now()
	f.run.StartedAt, f.run.FinishedAt = &started, &finished
	for i := range f.run.StageRuns {
		sr := &f.run.StageRuns[i]
		sr.Status = model.RunStatusSuccess
		sr.StartedAt, sr.FinishedAt = &started, &finished
		if f.p.Stages[i].Type == model.StageTypeDeploy && f.p.Stages[i].Config.DeployRuntime == "docker" {
			stage := f.p.Stages[i]
			effectiveName := stage.Config.RuntimeName
			if effectiveName == "" {
				effectiveName = stage.Config.ComposeServiceName
			}
			if effectiveName == "" {
				effectiveName = "cooker-service"
			}
			sr.RuntimeReceipt = &model.RuntimeReceipt{Version: 1, Runtime: "docker", RunID: f.run.ID, StageID: stage.ID,
				ContainerID: strings.Repeat("a", 64), Name: effectiveName, ImageReference: stage.Config.Image,
				ImageID: "sha256:" + strings.Repeat("b", 64), CapturedAt: started.Add(time.Second)}
		}
	}
	f.payload = runJobPayload{PipelineID: f.p.ID, RunID: f.run.ID, Pipeline: f.p, Run: f.planned}
	f.jobs.job = &jobqueue.Job{ID: "run:" + f.run.ID, Kind: JobKindPipelineRun, Status: jobqueue.StatusSucceeded, ConcurrencyKey: "pipeline:" + f.p.ID}
}

func (f *nativeRuntimeFixture) persist(t *testing.T) {
	t.Helper()
	b, err := json.Marshal(f.payload)
	if err != nil {
		t.Fatal(err)
	}
	f.jobs.job.Payload = b
	if err := f.r.Store.Runs.Create(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
}

func (f *nativeRuntimeFixture) response(t *testing.T, rr *model.RuntimeReceipt, state, configured, health string) {
	t.Helper()
	fields := []string{rr.ContainerID, "/" + rr.Name, rr.ImageID, rr.ImageReference, rr.RunID, rr.StageID, state, configured, health}
	b, _ := json.Marshal(fields)
	if err := os.WriteFile(filepath.Join(f.dir, rr.ContainerID+".json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *nativeRuntimeFixture) subject() NativeRuntimeSubject {
	return NativeRuntimeSubject{Kind: "native-stage", PipelineID: f.p.ID, RunID: f.run.ID, StageID: "deploy-web"}
}

func (f *nativeRuntimeFixture) noInspect(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(f.calls); !os.IsNotExist(err) {
		t.Fatal("unverified input reached runtime CLI")
	}
}

func TestNativeStageRuntimeAcceptedBindingAndSafeInspect(t *testing.T) {
	f := newNativeRuntimeFixture(t)
	f.persist(t)
	s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
	if err != nil || s.Binding != "verified" || s.RecordedStageStatus != model.RunStatusSuccess || s.State != "running" || s.Health != "healthy" || s.Subject != f.subject() || s.ContainerID != strings.Repeat("a", 64) || s.ObservedAt == nil || s.ObservedAt.Location() != time.UTC || !reflect.DeepEqual(s.SupportedActions, []string{"inspect"}) {
		t.Fatal("accepted exact binding/status not preserved", s, err)
	}
	data, _ := os.ReadFile(f.calls)
	if string(data) != "container\ninspect\n--format\n"+nativeRuntimeInspectFormat+"\n"+s.ContainerID+"\n" || strings.Contains(string(data), ".Config.Env") || strings.Contains(string(data), ".State.Health.Log") || strings.Contains(string(data), "{{json .Config.Labels}}") {
		t.Fatal("inspect did not use exact ID and safe explicit fields")
	}
	apps, _ := f.r.Store.Apps.List(context.Background(), 0, 0)
	if len(apps) != 0 {
		t.Fatal("native observation manufactured an App")
	}
}

func TestNativeStageRuntimeDefinitionEditAndDeletion(t *testing.T) {
	f := newNativeRuntimeFixture(t)
	f.persist(t)
	current, _ := f.r.Store.Pipelines.Get(context.Background(), f.p.ID)
	current.Stages = []model.Stage{{ID: "replacement", Type: model.StageTypeApproval}}
	current.Source = &model.PipelineSourceConfig{ConnectionID: "replacement", Repository: "other/repo", FullRef: "refs/heads/changed"}
	if err := f.r.Store.Pipelines.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
	if err != nil || s.Binding != "verified" || s.Name != "web" {
		t.Fatal("current definition replaced accepted authority", s, err)
	}
	if err := f.r.Store.Pipelines.Delete(context.Background(), f.p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs); !errors.Is(err, ErrNativeRuntimeNotFound) {
		t.Fatal("retained queue resurrected deleted pipeline", err)
	}
}

func TestNativeStageRuntimeRejectsAcceptedInputSubstitution(t *testing.T) {
	for _, scenario := range []string{"missing-job", "foreign-kind", "foreign-key", "payload-pipeline", "payload-run", "snapshot-pipeline", "snapshot-run", "missing-pipeline", "missing-run", "version", "actor", "groups", "pending-status", "pending-start", "pending-receipt", "duplicate-stage", "duplicate-stage-run", "duplicate-pending-stage", "legacy-version", "malformed-payload"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeRuntimeFixture(t)
			switch scenario {
			case "foreign-kind":
				f.jobs.job.Kind = JobKindAppDeploy
			case "foreign-key":
				f.jobs.job.ConcurrencyKey = "app:foreign"
			case "payload-pipeline":
				f.payload.PipelineID = "foreign"
			case "payload-run":
				f.payload.RunID = "foreign"
			case "snapshot-pipeline":
				f.p.ID = "foreign"
			case "snapshot-run":
				f.planned.ID = "foreign"
			case "missing-pipeline":
				f.payload.Pipeline = nil
			case "missing-run":
				f.payload.Run = nil
			case "version":
				f.run.PipelineVersion++
			case "actor":
				f.run.StartedByUserSub = "foreign"
			case "groups":
				f.run.StartedByGroups = []string{"different"}
			case "pending-status":
				f.planned.Status = model.RunStatusRunning
			case "pending-start":
				now := time.Now()
				f.planned.StartedAt = &now
			case "pending-receipt":
				f.planned.StageRuns[0].RuntimeReceipt = f.run.StageRuns[0].RuntimeReceipt
			case "duplicate-stage":
				f.p.Stages = append(f.p.Stages, f.p.Stages[0])
			case "duplicate-stage-run":
				f.run.StageRuns = append(f.run.StageRuns, f.run.StageRuns[0])
			case "duplicate-pending-stage":
				f.planned.StageRuns = append(f.planned.StageRuns, f.planned.StageRuns[0])
			case "legacy-version":
				f.p.Version = 0
				f.run.PipelineVersion = 0
				f.planned.PipelineVersion = 0
			}
			f.persist(t)
			if scenario == "missing-job" {
				f.jobs.job = nil
			}
			if scenario == "malformed-payload" {
				f.jobs.job.Payload = []byte(`{"private":"private-payload-canary"`)
			}
			subject := f.subject()
			subject.PipelineID = "native-pipeline"
			s, err := f.r.NativeStageStatus(context.Background(), subject, f.jobs)
			if err != nil || s.Binding != "unavailable" || s.ContainerID != "" || s.Health != "unknown" {
				t.Fatal("substituted acceptance reached verified observation", scenario, s, err)
			}
			f.noInspect(t)
		})
	}
}

func TestNativeStageRuntimeSubjectOwnershipAndMissingInfrastructure(t *testing.T) {
	f := newNativeRuntimeFixture(t)
	f.persist(t)
	for _, subject := range []NativeRuntimeSubject{{Kind: "app-service", PipelineID: f.p.ID, RunID: f.run.ID, StageID: "deploy-web"}, {Kind: "native-stage", PipelineID: "foreign", RunID: f.run.ID, StageID: "deploy-web"}, {Kind: "native-stage", PipelineID: f.p.ID, RunID: "foreign", StageID: "deploy-web"}, {Kind: "native-stage", PipelineID: f.p.ID, RunID: f.run.ID, StageID: "foreign"}} {
		if _, err := f.r.NativeStageStatus(context.Background(), subject, f.jobs); !errors.Is(err, ErrNativeRuntimeRequest) && !errors.Is(err, ErrNativeRuntimeNotFound) {
			t.Fatal("foreign subject did not fail safely", err)
		}
	}
	if _, err := f.r.NativeStageStatus(context.Background(), f.subject(), nil); !errors.Is(err, ErrNativeRuntimeUnavailable) {
		t.Fatal("missing queue did not fail closed", err)
	}
	var absent *RuntimeService
	if _, err := absent.NativeStageStatus(context.Background(), f.subject(), f.jobs); !errors.Is(err, ErrNativeRuntimeUnavailable) {
		t.Fatal("missing runtime service did not fail safely", err)
	}
	f.jobs.err = errors.New("private-queue-error-canary")
	if _, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs); !errors.Is(err, ErrNativeRuntimeUnavailable) || strings.Contains(err.Error(), "canary") {
		t.Fatal("raw queue error escaped", err)
	}
	f.noInspect(t)
}

func TestNativeStageRuntimeJobAndBusinessStatusMatrix(t *testing.T) {
	for _, tc := range []struct {
		job     jobqueue.Status
		run     model.RunStatus
		allowed bool
	}{
		{jobqueue.StatusRunning, model.RunStatusRunning, true}, {jobqueue.StatusSucceeded, model.RunStatusSuccess, true},
		{jobqueue.StatusSucceeded, model.RunStatusFailed, true}, {jobqueue.StatusSucceeded, model.RunStatusCancelled, true},
		{jobqueue.StatusPending, model.RunStatusRunning, false}, {jobqueue.StatusFailed, model.RunStatusFailed, false},
		{jobqueue.StatusCancelled, model.RunStatusCancelled, false}, {jobqueue.StatusNeedsAttention, model.RunStatusNeedsAttention, false},
		{jobqueue.StatusRunning, model.RunStatusSuccess, false}, {jobqueue.StatusSucceeded, model.RunStatusRunning, false},
		{jobqueue.StatusSucceeded, model.RunStatusNeedsAttention, false}, {jobqueue.StatusSucceeded, model.RunStatusPending, false},
	} {
		t.Run(string(tc.job)+"-"+string(tc.run), func(t *testing.T) {
			f := newNativeRuntimeFixture(t)
			f.jobs.job.Status = tc.job
			f.run.Status = tc.run
			f.persist(t)
			s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
			if err != nil || (s.Binding == "verified") != tc.allowed || s.RecordedStageStatus != model.RunStatusSuccess {
				t.Fatal("job/run lifecycle was conflated with deployment operation", s, err)
			}
			if !tc.allowed {
				f.noInspect(t)
			}
		})
	}
}

func TestNativeStageRuntimeLegacyAndUnsupportedRemainUnavailable(t *testing.T) {
	for _, scenario := range []string{"missing-receipt", "warning", "review-only", "compose", "kubernetes", "build", "pending", "failed", "skipped", "running", "receipt-run", "receipt-stage", "receipt-name", "receipt-image", "receipt-short-id", "receipt-null-time", "implicit-built-image"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeRuntimeFixture(t)
			rr := f.run.StageRuns[0].RuntimeReceipt
			switch scenario {
			case "missing-receipt":
				f.run.StageRuns[0].RuntimeReceipt = nil
			case "warning":
				f.run.StageRuns[0].RuntimeBindingWarning = "private-warning-canary"
			case "review-only":
				f.p.Stages[0].Config.ReviewOnly = true
			case "compose", "kubernetes":
				f.p.Stages[0].Config.DeployRuntime = scenario
			case "build":
				f.p.Stages[0].Type = model.StageTypeBuild
			case "pending", "failed", "skipped", "running":
				f.run.StageRuns[0].Status = model.RunStatus(scenario)
			case "receipt-run":
				rr.RunID = "foreign"
			case "receipt-stage":
				rr.StageID = "foreign"
			case "receipt-name":
				rr.Name = "foreign"
			case "receipt-image":
				rr.ImageReference = "registry.invalid/foreign:v1"
			case "receipt-short-id":
				rr.ContainerID = "aaaa"
			case "receipt-null-time":
				rr.CapturedAt = time.Time{}
			case "implicit-built-image":
				f.p.Stages[0].Config.Image = ""
			}
			f.persist(t)
			s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
			b, _ := json.Marshal(s)
			if err != nil || s.Binding != "unavailable" || s.ContainerID != "" || strings.Contains(string(b), "canary") {
				t.Fatal("unrecorded/unsupported stage invented runtime state", s, err)
			}
			f.noInspect(t)
		})
	}
}

func TestNativeStageRuntimeRejectsExactInspectIdentitySubstitution(t *testing.T) {
	for i := 0; i < 6; i++ {
		t.Run([]string{"container", "name", "content-image", "reference", "run", "stage"}[i], func(t *testing.T) {
			f := newNativeRuntimeFixture(t)
			f.persist(t)
			path := filepath.Join(f.dir, strings.Repeat("a", 64)+".json")
			b, _ := os.ReadFile(path)
			var fields []string
			_ = json.Unmarshal(b, &fields)
			fields[i] = "private-inspect-canary"
			b, _ = json.Marshal(fields)
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
			public, _ := json.Marshal(s)
			if err != nil || s.Binding != "unavailable" || s.ContainerID != "" || s.ReasonCode != "runtime_identity_mismatch" || strings.Contains(string(public), "canary") {
				t.Fatal("identity substitution escaped safe refusal", s, err)
			}
		})
	}
}

func TestNativeStageRuntimeSameNameDistinctReceiptsAndRemovedOldID(t *testing.T) {
	f := newNativeRuntimeFixture(t)
	f.p.Stages = append(f.p.Stages, *receiptStage("deploy-next", "web"))
	f.resetRun()
	f.run.StageRuns[1].RuntimeReceipt.ContainerID = strings.Repeat("c", 64)
	f.response(t, f.run.StageRuns[1].RuntimeReceipt, "running", "absent", "")
	if err := os.Remove(filepath.Join(f.dir, strings.Repeat("a", 64)+".json")); err != nil {
		t.Fatal(err)
	}
	f.persist(t)
	old, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
	if err != nil || old.Binding != "unavailable" {
		t.Fatal("removed old ID followed a reused name", old, err)
	}
	next := f.subject()
	next.StageID = "deploy-next"
	current, err := f.r.NativeStageStatus(context.Background(), next, f.jobs)
	if err != nil || current.Binding != "verified" || current.ContainerID != strings.Repeat("c", 64) || current.Name != "web" {
		t.Fatal("distinct same-name stage rejected", current, err)
	}
	data, _ := os.ReadFile(f.calls)
	if strings.Contains(string(data), "\nweb\n") {
		t.Fatal("name fallback used")
	}
}

func TestNativeStageRuntimeDuplicateReceiptIDsAreUnavailable(t *testing.T) {
	f := newNativeRuntimeFixture(t)
	f.p.Stages = append(f.p.Stages, *receiptStage("deploy-next", "other"))
	f.resetRun()
	f.persist(t)
	s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
	if err != nil || s.Binding != "unavailable" {
		t.Fatal("one container claimed by multiple stages", s, err)
	}
	f.noInspect(t)
}

func TestNativeStageRuntimeInterpolationUsesOnlyAcceptedSuccessfulAncestors(t *testing.T) {
	for _, scenario := range []string{"accepted", "current-flag-disabled", "missing-output", "failed-ancestor", "sibling-output", "wrong-receipt-image"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeRuntimeFixture(t)
			deploy := *receiptStage("deploy-web", "web")
			deploy.Config.Image = "${stages.push.image}"
			f.p.Stages = []model.Stage{{ID: "push", Type: model.StageTypePush}, deploy}
			f.p.Edges = []model.Edge{{Source: "push", Target: "deploy-web"}}
			f.resetRun()
			f.run.StageRuns[0].Outputs = map[string]string{"image": "registry.invalid/web@sha256:" + strings.Repeat("d", 64)}
			f.run.StageRuns[1].RuntimeReceipt.ImageReference = f.run.StageRuns[0].Outputs["image"]
			switch scenario {
			case "current-flag-disabled":
				t.Setenv("COOKER_OUTPUTS_ENABLED", "false")
			case "missing-output":
				f.run.StageRuns[0].Outputs = nil
			case "failed-ancestor":
				f.run.StageRuns[0].Status = model.RunStatusFailed
			case "sibling-output":
				f.p.Edges = nil
			case "wrong-receipt-image":
				f.run.StageRuns[1].RuntimeReceipt.ImageReference = "registry.invalid/foreign:v1"
			}
			f.response(t, f.run.StageRuns[1].RuntimeReceipt, "running", "absent", "")
			f.persist(t)
			s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
			allowed := scenario == "accepted" || scenario == "current-flag-disabled"
			if err != nil || (s.Binding == "verified") != allowed {
				t.Fatal("interpolation followed mutable flag or unaccepted output", s, err)
			}
			if !allowed {
				f.noInspect(t)
			}
		})
	}
}

func nativeStatusSourcePlan(f *nativeRuntimeFixture) *model.PipelineSourcePlan {
	f.p.Source = &model.PipelineSourceConfig{ConnectionID: "accepted-connection", Repository: "owned/native", FullRef: "refs/heads/main"}
	f.p.Stages = append([]model.Stage{{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Dockerfile: "Dockerfile"}}}, f.p.Stages...)
	f.resetRun()
	plan := &model.PipelineSourcePlan{Version: 1, Source: model.GitSourceIdentity{Provider: "gitea", ConnectionID: "accepted-connection", Host: "gitea.example.invalid", RepositoryID: "provider-repo", Repository: "owned/native"}, ConnectionVersion: 1, FullRef: "refs/heads/main", CommitSHA: strings.Repeat("d", 40), ObjectSHA: strings.Repeat("d", 40), PipelineID: f.p.ID, PipelineVersion: f.p.Version, EnvironmentVersions: map[string]int{}, ReviewPolicy: "manual", InputFiles: []model.SourceInputHash{{Path: "Dockerfile", Present: true, SHA256: strings.Repeat("e", 64)}}}
	plan.PlanHash = pipelineSourcePlanHash(f.p, plan)
	b, _ := json.Marshal(plan.InputFiles)
	hash := sha256.Sum256(b)
	plan.EnvelopeHash = hex.EncodeToString(hash[:])
	return plan
}

func TestNativeStageRuntimeSourceLineageUsesAcceptedProofWithoutProviderReads(t *testing.T) {
	for _, scenario := range []string{"accepted", "missing-checkout", "wrong-commit", "wrong-digest", "wrong-plan", "admission-checkout", "missing-plan", "missing-automatic-proof"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeRuntimeFixture(t)
			plan := nativeStatusSourcePlan(f)
			lineage, err := model.RunSourceFromAcceptedPlan(plan)
			if err != nil {
				t.Fatal(err)
			}
			f.payload.Source = plan
			f.planned.Source = lineage.Clone()
			f.run.Source = lineage.Clone()
			now := time.Now()
			f.run.Source.CheckedOutAt = &now
			switch scenario {
			case "missing-checkout":
				f.run.Source.CheckedOutAt = nil
			case "wrong-commit":
				f.run.Source.CommitSHA = strings.Repeat("c", 40)
			case "wrong-digest":
				f.run.Source.SourcePlanDigest = strings.Repeat("c", 64)
			case "wrong-plan":
				plan.ConnectionVersion++
			case "admission-checkout":
				f.planned.Source.CheckedOutAt = &now
			case "missing-plan":
				f.payload.Source = nil
			case "missing-automatic-proof":
				plan.ReviewPolicy = "automatic"
				plan.PlanHash = pipelineSourcePlanHash(f.p, plan)
			}
			f.response(t, f.run.StageRuns[1].RuntimeReceipt, "running", "absent", "")
			f.persist(t)
			s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
			if err != nil || (s.Binding == "verified") != (scenario == "accepted") {
				t.Fatal("source lineage/checkout was invented or replaced", s, err)
			}
			if scenario != "accepted" {
				f.noInspect(t)
			}
		})
	}
}

func TestNativeStageRuntimeSecretMetadataExactWithoutSecretResolution(t *testing.T) {
	for _, scenario := range []string{"database", "external", "missing", "wrong-stage", "wrong-key", "wrong-environment", "extra-environment", "wrong-reference-scope", "unexpected-plan"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeRuntimeFixture(t)
			f.p.Stages[0].EnvironmentID = "accepted-env"
			f.p.Stages[0].Config.SecretRefs = []string{"TOKEN"}
			pin := &EnvironmentSecretPlan{Version: 1, EnvironmentID: "accepted-env", EnvironmentVersion: 1, Mode: "database", Keys: []string{"TOKEN"}}
			plan := &PipelineSecretPlan{Version: 1, Stages: map[string]StageSecretBinding{"deploy-web": {EnvironmentID: "accepted-env", Keys: []string{"TOKEN"}}}, Environments: map[string]*EnvironmentSecretPlan{"accepted-env": pin}}
			f.payload.Secrets = plan
			switch scenario {
			case "external", "wrong-reference-scope":
				pin.Mode = "external"
				pin.References = map[string]secrets.VersionReference{"TOKEN": {SchemaVersion: 1, Provider: "vault", Resource: "accepted-env/TOKEN", Version: "1", Scope: secrets.VersionScope{Authority: "https://vault.example.invalid", EnvironmentID: "accepted-env", Key: "TOKEN"}}}
				if scenario == "wrong-reference-scope" {
					ref := pin.References["TOKEN"]
					ref.Scope.EnvironmentID = "foreign"
					pin.References["TOKEN"] = ref
				}
			case "missing":
				f.payload.Secrets = nil
			case "wrong-stage":
				plan.Stages = map[string]StageSecretBinding{"foreign": {EnvironmentID: "accepted-env", Keys: []string{"TOKEN"}}}
			case "wrong-key":
				pin.Keys = []string{"OTHER"}
			case "wrong-environment":
				pin.EnvironmentID = "foreign"
			case "extra-environment":
				plan.Environments["foreign"] = pin
			case "unexpected-plan":
				f.p.Stages[0].Config.SecretRefs = nil
			}
			// No environment or secret manager exists in the runtime. This
			// proves metadata validation does not enumerate/decrypt credentials.
			f.persist(t)
			s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
			allowed := scenario == "database" || scenario == "external"
			if err != nil || (s.Binding == "verified") != allowed {
				t.Fatal("secret admission metadata was replaced or resolved", s, err)
			}
			if !allowed {
				f.noInspect(t)
			}
		})
	}
}

func TestNativeStageRuntimeStateAndHealthRemainIndependent(t *testing.T) {
	for _, tc := range []struct{ state, configured, observed, expectedState, expectedHealth string }{
		{"running", "absent", "", "running", "not_configured"}, {"running", "configured", "healthy", "running", "healthy"},
		{"running", "configured", "unhealthy", "running", "unhealthy"}, {"running", "configured", "starting", "running", "starting"},
		{"running", "configured", "", "running", "unknown"}, {"created", "configured", "", "created", "unknown"},
		{"exited", "configured", "", "exited", "unknown"}, {"exited", "configured", "healthy", "exited", "unknown"},
		{"restarting", "configured", "healthy", "restarting", "unknown"}, {"running", "disabled", "", "running", "not_configured"},
		{"running", "unknown", "healthy", "running", "unknown"}, {"running", "private-config-canary", "healthy", "running", "unknown"},
		{"private-state-canary", "configured", "private-health-canary", "unknown", "unknown"},
	} {
		t.Run(tc.state+"-"+tc.observed, func(t *testing.T) {
			f := newNativeRuntimeFixture(t)
			f.response(t, f.run.StageRuns[0].RuntimeReceipt, tc.state, tc.configured, tc.observed)
			f.persist(t)
			s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
			b, _ := json.Marshal(s)
			if err != nil || s.Binding != "verified" || s.RecordedStageStatus != model.RunStatusSuccess || s.State != tc.expectedState || s.Health != tc.expectedHealth || strings.Contains(string(b), "canary") {
				t.Fatal("deployment, state and health were conflated or raw data escaped", s, err)
			}
		})
	}
}

func TestNativeStageRuntimeHealthConfigurationProjectionIsSafe(t *testing.T) {
	type healthConfig struct{ Test []string }
	type config struct {
		Image       string
		Labels      map[string]string
		Healthcheck *healthConfig
	}
	type healthState struct {
		Status string
		Log    []string
	}
	type state struct {
		Status string
		Health *healthState
	}
	format, err := template.New("native-safe-inspect").Funcs(template.FuncMap{"json": func(v any) string { b, _ := json.Marshal(v); return string(b) }}).Parse(nativeRuntimeInspectFormat)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                           string
		check                          *healthConfig
		health                         *healthState
		expectedConfig, expectedResult string
	}{
		{"absent", nil, nil, "absent", ""},
		{"disabled", &healthConfig{Test: []string{"NONE"}}, nil, "disabled", ""},
		{"configured-missing-state", &healthConfig{Test: []string{"CMD", "private-command-canary"}}, nil, "configured", ""},
		{"shell-configured", &healthConfig{Test: []string{"CMD-SHELL", "private-command-canary"}}, &healthState{Status: "healthy", Log: []string{"private-health-log-canary"}}, "configured", "healthy"},
		{"empty-test", &healthConfig{}, nil, "unknown", ""},
		{"unknown-test", &healthConfig{Test: []string{"private-command-canary"}}, nil, "unknown", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := struct {
				Id, Name, Image string
				Config          config
				State           state
			}{
				Id: strings.Repeat("a", 64), Name: "/web", Image: "sha256:" + strings.Repeat("b", 64),
				Config: config{Image: "registry.invalid/web:v1", Labels: map[string]string{RuntimeRunLabel: "native-run", RuntimeStageLabel: "deploy-web"}, Healthcheck: tc.check},
				State:  state{Status: "created", Health: tc.health}}
			var output bytes.Buffer
			if err := format.Execute(&output, data); err != nil {
				t.Fatal(err)
			}
			var fields []string
			if err := json.Unmarshal(output.Bytes(), &fields); err != nil || len(fields) != 9 || fields[7] != tc.expectedConfig || fields[8] != tc.expectedResult || strings.Contains(output.String(), "canary") {
				t.Fatal("safe projection confused configuration/state or exposed command/log", err)
			}
		})
	}
}

func TestNativeStageRuntimeInspectBoundsCancellationAndSafeFailures(t *testing.T) {
	for _, scenario := range []string{"fail", "missing-bin", "invalid-json", "oversized", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeRuntimeFixture(t)
			f.persist(t)
			ctx := context.Background()
			switch scenario {
			case "fail":
				t.Setenv("COOKER_NATIVE_MODE", "fail")
			case "missing-bin":
				f.r.DockerBin = filepath.Join(f.dir, "absent")
			case "invalid-json":
				if err := os.WriteFile(filepath.Join(f.dir, strings.Repeat("a", 64)+".json"), []byte("private-json-canary"), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(filepath.Join(f.dir, strings.Repeat("a", 64)+".json"), []byte(strings.Repeat("private-size-canary", 8192)), 0600); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				t.Setenv("COOKER_NATIVE_MODE", "wait")
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			s, err := f.r.NativeStageStatus(ctx, f.subject(), f.jobs)
			b, _ := json.Marshal(s)
			if err != nil || s.Binding != "unavailable" || s.ContainerID != "" || s.ObservedAt != nil || strings.Contains(string(b), "canary") {
				t.Fatal("inspect failure became verified or leaked raw diagnostics", s, err)
			}
		})
	}
	if _, bypass := any(&boundedRuntimeLogBuffer{limit: 64 * 1024}).(io.ReaderFrom); bypass {
		t.Fatal("inspect bound bypassed by promoted ReaderFrom")
	}
}

func TestNativeStageRuntimeInspectDescendantPipeWaitIsBounded(t *testing.T) {
	f := newNativeRuntimeFixture(t)
	f.persist(t)
	t.Setenv("COOKER_NATIVE_MODE", "descendant")
	t.Cleanup(func() {
		if err := os.WriteFile(filepath.Join(f.dir, "release"), []byte("release"), 0600); err != nil {
			t.Error(err)
			return
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			if b, err := os.ReadFile(filepath.Join(f.dir, "done")); err == nil && string(b) == "done" {
				return
			}
			if time.Now().After(deadline) {
				t.Error("owned descendant did not finish")
				return
			}
			time.Sleep(time.Millisecond)
		}
	})
	s, err := f.r.NativeStageStatus(context.Background(), f.subject(), f.jobs)
	if err != nil || s.Binding != "unavailable" {
		t.Fatal("optional inspect did not bound descendant pipes", s, err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "done")); !os.IsNotExist(err) {
		t.Fatal("inspect waited for descendant instead of finite pipe deadline")
	}
}

func TestNativeStageRuntimeKnownSecretReceiptMetadataIsWithheld(t *testing.T) {
	f := newNativeRuntimeFixture(t)
	f.persist(t)
	ctx := WithLogSecrets(context.Background(), []string{strings.Repeat("b", 64)})
	s, err := f.r.NativeStageStatus(ctx, f.subject(), f.jobs)
	b, _ := json.Marshal(s)
	if err != nil || s.Binding != "unavailable" || strings.Contains(string(b), strings.Repeat("b", 64)) {
		t.Fatal("known secret receipt metadata entered status", s, err)
	}
	f.noInspect(t)
}
