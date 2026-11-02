package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/store"
)

type nativeLogFixture struct {
	*nativeRuntimeFixture
	resolver *AppEnvResolver
	manager  *nativeLogBoundSecrets
}

// The accepted-input fixture is source-only unit data. The task-owned CLI
// proves exact command selection, redaction and finite pipe behavior; no
// worker, provider, Docker daemon or genuine deployment is substituted here.
func newNativeLogFixture(t *testing.T) *nativeLogFixture {
	t.Helper()
	f := &nativeLogFixture{nativeRuntimeFixture: newNativeRuntimeFixture(t)}
	cli := filepath.Join(f.dir, "docker-native-logs")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$COOKER_NATIVE_LOG_DIR/calls"
if [ "$1" = container ]; then
 [ "$2" = inspect ] && [ "$3" = --format ] || exit 91
 for last; do :; done
 cat "$COOKER_NATIVE_LOG_DIR/$last.json"
 exit $?
fi
[ "$1" = logs ] || exit 92
case "$COOKER_NATIVE_LOG_MODE" in
 fail) printf '%s' private-log-cli-error-canary >&2; exit 44;;
 wait) printf '%s' started > "$COOKER_NATIVE_LOG_DIR/live-started"; printf '%s\n' stream-marker; exec sleep 60;;
 descendant) (count=0; while [ ! -f "$COOKER_NATIVE_LOG_DIR/release" ] && [ "$count" -lt 100 ]; do sleep 0.02; count=$((count + 1)); done; printf '%s' done > "$COOKER_NATIVE_LOG_DIR/done") &;;
 overflow) dd if=/dev/zero bs=65536 count=17 2>/dev/null | tr '\000' x; exit 0;;
 lines) i=0; while [ "$i" -lt 10001 ]; do printf '%s\n' line; i=$((i + 1)); done; exit 0;;
 split) printf '%s' selected-; sleep 0.02; printf '%s\n' secret-canary >&2; printf '%s\n' ordinary-config; exit 0;;
esac
cat "$COOKER_NATIVE_LOG_DIR/logs"
`
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	f.r.DockerBin = cli
	t.Setenv("COOKER_NATIVE_LOG_DIR", f.dir)
	t.Setenv("COOKER_NATIVE_LOG_MODE", "")
	f.logs(t, "2026-10-01T00:01:00Z selected-secret-canary\n2026-10-01T00:02:00Z ordinary-config\n2026-10-01T00:03:00Z third\n")
	return f
}

func (f *nativeLogFixture) logs(t *testing.T, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, "logs"), []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
}

type nativeLogBoundSecrets struct {
	secrets.Manager
	values map[string][]byte
	gets   []string
	onGet  func(context.Context, string)
	err    error
}

func (s *nativeLogBoundSecrets) EnvironmentGenerationBound() bool { return true }
func (s *nativeLogBoundSecrets) Get(ctx context.Context, env, key string) ([]byte, error) {
	s.gets = append(s.gets, env+"/"+key)
	if s.onGet != nil {
		s.onGet(ctx, key)
	}
	if s.err != nil {
		return nil, s.err
	}
	v, ok := s.values[key]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	return append([]byte{}, v...), nil
}
func (*nativeLogBoundSecrets) List(context.Context, string) ([]string, error) {
	panic("native logs must never list secrets")
}

func (f *nativeLogFixture) selectedDatabase(t *testing.T) {
	t.Helper()
	env := &model.Environment{ID: "accepted-env", Name: "Accepted", Version: 1, PlainVars: map[string]string{"PUBLIC": "ordinary-config"}}
	if err := f.r.Store.Environments.Create(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	f.p.Stages[0].EnvironmentID = env.ID
	f.p.Stages[0].Config.SecretRefs = []string{"TOKEN"}
	f.payload.Secrets = &PipelineSecretPlan{Version: 1, Stages: map[string]StageSecretBinding{"deploy-web": {EnvironmentID: env.ID, Keys: []string{"TOKEN"}}},
		Environments: map[string]*EnvironmentSecretPlan{env.ID: {Version: 1, EnvironmentID: env.ID, EnvironmentVersion: 1, Mode: "database", Keys: []string{"TOKEN"}}}}
	f.manager = &nativeLogBoundSecrets{values: map[string][]byte{"TOKEN": []byte("selected-secret-canary"), "OTHER": []byte("unselected-secret-canary")}}
	f.resolver = &AppEnvResolver{Environments: f.r.Store.Environments, Secrets: f.manager}
}

func (f *nativeLogFixture) noLogs(t *testing.T) {
	t.Helper()
	b, _ := os.ReadFile(f.calls)
	if strings.Contains(string(b), "logs\n") {
		t.Fatal("unadmitted input reached runtime logs")
	}
}

func nativeLogNoBytes(t *testing.T, page NativeRuntimeLogPage, err error) {
	t.Helper()
	if err == nil || page.SessionID != "" || len(page.Entries) != 0 || strings.Contains(err.Error(), "canary") {
		t.Fatal("failed native log request exposed output or private diagnostics", err)
	}
}

func TestNativeStageLogsSelectedRedactionStablePagingAndDTO(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	f.persist(t)
	replicas, err := f.r.NativeStageLogReplicas(context.Background(), f.subject(), f.jobs)
	if err != nil || replicas.Subject != f.subject() || replicas.RunID != f.run.ID || len(replicas.Replicas) != 1 || replicas.Replicas[0].ContainerID != strings.Repeat("a", 64) || len(f.manager.gets) != 0 {
		t.Fatal("replica identity required credentials or drifted", err)
	}
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
	if err != nil || page.Subject != f.subject() || page.RunID != f.run.ID || page.Total != 3 || page.Entries[0].Message != "[REDACTED]" || page.Entries[1].Message != "ordinary-config" || page.ContainerID != replicas.Replicas[0].ContainerID {
		t.Fatal("selected protected snapshot failed", err, page)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), `"appId"`) || strings.Contains(string(encoded), `"service"`) || strings.Contains(string(encoded), "selected-secret-canary") || strings.Contains(string(encoded), "accepted-env") {
		t.Fatal("native DTO manufactured App identity or exposed private metadata")
	}
	p1, err := f.r.ReadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver, 1, 1)
	if err != nil || len(p1.Entries) != 1 || p1.Entries[0].Sequence != 2 || p1.NextOffset == nil || *p1.NextOffset != 2 {
		t.Fatal("native stable cursor failed", err)
	}
	p2, err := f.r.ReadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver, 1, 1)
	if err != nil || !reflect.DeepEqual(p1, p2) {
		t.Fatal("repeated native page changed", err)
	}
	p1.Entries[0].Message = "caller-mutated"
	download, err := f.r.DownloadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver)
	if err != nil || strings.Contains(download, "canary") || strings.Contains(download, "caller-mutated") || !strings.Contains(download, "[REDACTED]") || !strings.Contains(download, "ordinary-config") {
		t.Fatal("download bypassed immutable redaction", err)
	}
	for _, key := range f.manager.gets {
		if key != "accepted-env/TOKEN" {
			t.Fatal("unselected secret read")
		}
	}
	calls, _ := os.ReadFile(f.calls)
	if strings.Count(string(calls), "logs\n") != 1 || strings.Contains(string(calls), "\nweb\n") || !strings.Contains(string(calls), "--timestamps\n--tail\n2000\n") {
		t.Fatal("cached reads reopened logs or used a mutable name")
	}
	apps, _ := f.r.Store.Apps.List(context.Background(), 0, 0)
	if len(apps) != 0 {
		t.Fatal("native log view created an App")
	}
}

func TestNativeStageLogsSubsetUnionAndExplicitNoReferences(t *testing.T) {
	for _, selected := range []bool{true, false} {
		t.Run(fmt.Sprint(selected), func(t *testing.T) {
			f := newNativeLogFixture(t)
			f.selectedDatabase(t)
			f.p.Stages = append(f.p.Stages, model.Stage{ID: "other-stage", Type: model.StageTypeCustom, EnvironmentID: "accepted-env", Config: model.StageConfig{SecretRefs: []string{"OTHER"}}})
			f.resetRun()
			f.payload.Secrets = &PipelineSecretPlan{Version: 1, Stages: map[string]StageSecretBinding{
				"deploy-web": {EnvironmentID: "accepted-env", Keys: []string{"TOKEN"}}, "other-stage": {EnvironmentID: "accepted-env", Keys: []string{"OTHER"}}},
				Environments: map[string]*EnvironmentSecretPlan{"accepted-env": {Version: 1, EnvironmentID: "accepted-env", EnvironmentVersion: 1, Mode: "database", Keys: []string{"OTHER", "TOKEN"}}}}
			if !selected {
				f.p.Stages[0].Config.SecretRefs = nil
				delete(f.payload.Secrets.Stages, "deploy-web")
				f.payload.Secrets.Environments["accepted-env"].Keys = []string{"OTHER"}
				f.resolver = nil
			}
			f.persist(t)
			page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
			if err != nil {
				t.Fatal("valid full-plan subset refused", err)
			}
			if selected {
				if page.Entries[0].Message != "[REDACTED]" || len(f.manager.gets) == 0 {
					t.Fatal("selected stage was not protected")
				}
				for _, key := range f.manager.gets {
					if key != "accepted-env/TOKEN" {
						t.Fatal("union key resolved for another stage")
					}
				}
				cached := f.r.runtimeLogCache().sessions[page.SessionID]
				if !reflect.DeepEqual(cached.native.Generation.Keys, []string{"TOKEN"}) || len(f.payload.Secrets.Environments["accepted-env"].Keys) != 2 {
					t.Fatal("subset cache mutated or retained union authority")
				}
			} else if len(f.manager.gets) != 0 || f.r.runtimeLogCache().sessions[page.SessionID].native.Generation != nil {
				t.Fatal("no-reference stage discovered credentials")
			}
		})
	}
}

func TestNativeStageLogsInvalidUnrelatedSecretPlanRefusesBeforeReads(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	f.p.Stages = append(f.p.Stages, model.Stage{ID: "other-stage", Type: model.StageTypeCustom, EnvironmentID: "accepted-env", Config: model.StageConfig{SecretRefs: []string{"OTHER"}}})
	f.resetRun()
	f.payload.Secrets = &PipelineSecretPlan{Version: 1, Stages: map[string]StageSecretBinding{"deploy-web": {EnvironmentID: "accepted-env", Keys: []string{"TOKEN"}}}, Environments: map[string]*EnvironmentSecretPlan{"accepted-env": {Version: 1, EnvironmentID: "accepted-env", EnvironmentVersion: 1, Mode: "database", Keys: []string{"TOKEN"}}}}
	f.persist(t)
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
	nativeLogNoBytes(t, page, err)
	f.noLogs(t)
	if len(f.manager.gets) != 0 {
		t.Fatal("selected values read before full plan validation")
	}
}

type nativeLogEnvHook struct {
	store.EnvironmentStore
	hook  func()
	fired bool
}

func (s *nativeLogEnvHook) Get(ctx context.Context, id string) (*model.Environment, error) {
	if !s.fired && s.hook != nil {
		s.hook()
	}
	return s.EnvironmentStore.Get(ctx, id)
}

func nativeLogRotate(t *testing.T, f *nativeLogFixture) {
	t.Helper()
	env, err := f.r.Store.Environments.Get(context.Background(), "accepted-env")
	if err != nil {
		t.Fatal(err)
	}
	copy := *env
	copy.PlainVars = map[string]string{"CHANGED": "yes"}
	if err = f.r.Store.Environments.Update(context.Background(), &copy); err != nil {
		t.Fatal(err)
	}
}

func TestNativeStageLogsRotationBeforeDuringAndCachedReads(t *testing.T) {
	for _, boundary := range []string{"before", "during", "cached"} {
		t.Run(boundary, func(t *testing.T) {
			f := newNativeLogFixture(t)
			f.selectedDatabase(t)
			f.persist(t)
			if boundary == "before" {
				nativeLogRotate(t, f)
			}
			if boundary == "during" {
				hook := &nativeLogEnvHook{EnvironmentStore: f.resolver.Environments}
				hook.hook = func() {
					calls, _ := os.ReadFile(f.calls)
					if strings.Contains(string(calls), "logs\n") {
						hook.fired = true
						nativeLogRotate(t, f)
					}
				}
				f.resolver.Environments = hook
			}
			page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
			if boundary != "cached" {
				nativeLogNoBytes(t, page, err)
				if len(f.r.runtimeLogCache().sessions) != 0 {
					t.Fatal("failed capture retained a session")
				}
				if boundary == "before" {
					f.noLogs(t)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			nativeLogRotate(t, f)
			p, err := f.r.ReadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver, 0, 10)
			nativeLogNoBytes(t, p, err)
			if data, err := f.r.DownloadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver); !errors.Is(err, ErrRuntimeLogUnavailable) || data != "" {
				t.Fatal("changed generation exposed cached download", err)
			}
		})
	}
}

func TestNativeStageLogsRotationWhileReadingSelectedValues(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	f.persist(t)
	f.manager.onGet = func(context.Context, string) { nativeLogRotate(t, f) }
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
	nativeLogNoBytes(t, page, err)
	f.noLogs(t)
}

func TestNativeStageLogsActualDatabaseManagerGeneration(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	manager := database.New(f.r.Store.Environments, codec)
	if err = manager.Put(context.Background(), "accepted-env", "TOKEN", []byte("selected-secret-canary")); err != nil {
		t.Fatal(err)
	}
	env, err := f.r.Store.Environments.Get(context.Background(), "accepted-env")
	if err != nil {
		t.Fatal(err)
	}
	f.payload.Secrets.Environments[env.ID].EnvironmentVersion = env.Version
	f.resolver.Secrets = manager
	f.persist(t)
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
	if err != nil || page.Entries[0].Message != "[REDACTED]" {
		t.Fatal("actual encrypted manager did not protect admitted generation", err)
	}
	if err = manager.Put(context.Background(), "accepted-env", "TOKEN", []byte("rotated-secret-canary")); err != nil {
		t.Fatal(err)
	}
	p, err := f.r.ReadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver, 0, 10)
	nativeLogNoBytes(t, p, err)
	if data, err := f.r.DownloadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver); err == nil || data != "" {
		t.Fatal("actual database rotation exposed cached bytes")
	}
}

func TestNativeStageLogsResolvedSecretMetadataIsWithheld(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	f.p.Stages[0].Config.RuntimeName = "selected-secret-canary"
	f.run.StageRuns[0].RuntimeReceipt.Name = "selected-secret-canary"
	f.response(t, f.run.StageRuns[0].RuntimeReceipt, "running", "configured", "healthy")
	f.persist(t)
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
	nativeLogNoBytes(t, page, err)
	f.noLogs(t)
	if len(f.r.runtimeLogCache().sessions) != 0 {
		t.Fatal("known secret receipt entered cache")
	}
}

func TestNativeStageLogsCachedReadsRevalidateAcceptedAuthority(t *testing.T) {
	for _, scenario := range []string{"receipt", "actor", "plan", "missing-job", "inspect"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeLogFixture(t)
			f.selectedDatabase(t)
			f.persist(t)
			page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "receipt":
				f.run.StageRuns[0].RuntimeReceipt.CapturedAt = f.run.StageRuns[0].RuntimeReceipt.CapturedAt.Add(time.Second)
			case "actor":
				f.run.StartedByUserSub = "foreign"
			case "plan":
				f.payload.Secrets.Environments["accepted-env"].Keys = []string{"OTHER"}
				payload, _ := json.Marshal(f.payload)
				f.jobs.job.Payload = payload
			case "missing-job":
				f.jobs.job = nil
			case "inspect":
				f.response(t, &model.RuntimeReceipt{ContainerID: strings.Repeat("a", 64), Name: "foreign", ImageID: "sha256:" + strings.Repeat("b", 64), ImageReference: "registry.invalid/web:v1", RunID: f.run.ID, StageID: "deploy-web"}, "running", "configured", "healthy")
			}
			if err = f.r.Store.Runs.Update(context.Background(), f.run); err != nil {
				t.Fatal(err)
			}
			p, err := f.r.ReadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver, 0, 10)
			nativeLogNoBytes(t, p, err)
			if data, err := f.r.DownloadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver); err == nil || data != "" {
				t.Fatal("invalidated accepted authority exposed cached bytes")
			}
		})
	}
}

type nativeLogExternalSecrets struct {
	secrets.Manager
	refs   map[string]secrets.VersionReference
	values map[string][]byte
	reads  []string
	lost   bool
}

func (*nativeLogExternalSecrets) Get(context.Context, string, string) ([]byte, error) {
	panic("native external logs must not get latest")
}
func (*nativeLogExternalSecrets) List(context.Context, string) ([]string, error) {
	panic("native external logs must not list")
}
func (*nativeLogExternalSecrets) Pin(context.Context, string, string) (secrets.VersionReference, error) {
	panic("native logs must not pin new versions")
}
func (s *nativeLogExternalSecrets) GetPinned(_ context.Context, env, key string, ref secrets.VersionReference) ([]byte, error) {
	s.reads = append(s.reads, env+"/"+key)
	if s.lost {
		return nil, errors.New("private-pinned-version-canary")
	}
	if ref != s.refs[key] || ref.Scope.EnvironmentID != env || ref.Scope.Key != key {
		return nil, secrets.ErrVersionMismatch
	}
	return append([]byte{}, s.values[key]...), nil
}

func TestNativeStageLogsExternalPinnedSubsetAndLostVersion(t *testing.T) {
	for _, scenario := range []string{"pinned", "lost", "unsupported", "bad-scope"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeLogFixture(t)
			f.selectedDatabase(t)
			pin := f.payload.Secrets.Environments["accepted-env"]
			pin.Mode = "external"
			ref := secrets.VersionReference{SchemaVersion: 1, Provider: "vault", Resource: "accepted-env/TOKEN", Version: "1", Scope: secrets.VersionScope{Authority: "https://vault.fixture.invalid", EnvironmentID: "accepted-env", Key: "TOKEN"}}
			pin.References = map[string]secrets.VersionReference{"TOKEN": ref}
			external := &nativeLogExternalSecrets{refs: map[string]secrets.VersionReference{"TOKEN": ref}, values: map[string][]byte{"TOKEN": []byte("selected-secret-canary")}, lost: scenario == "lost"}
			f.resolver.Secrets = external
			if scenario == "unsupported" {
				f.resolver.Secrets = &fakeSecrets{data: map[string]map[string][]byte{"accepted-env": {"TOKEN": []byte("latest-value")}}}
			}
			if scenario == "bad-scope" {
				ref.Scope.Key = "OTHER"
				pin.References["TOKEN"] = ref
			}
			f.persist(t)
			page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
			if scenario != "pinned" {
				nativeLogNoBytes(t, page, err)
				f.noLogs(t)
				return
			}
			if err != nil || page.Entries[0].Message != "[REDACTED]" || len(external.reads) == 0 {
				t.Fatal("accepted external version was not used", err)
			}
			// Newer provider inventory is irrelevant: no Get/List/Pin occurs.
			for _, key := range external.reads {
				if key != "accepted-env/TOKEN" {
					t.Fatal("external stage scope widened")
				}
			}
			external.lost = true
			p, err := f.r.ReadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver, 0, 10)
			nativeLogNoBytes(t, p, err)
			if data, err := f.r.DownloadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver); err == nil || data != "" {
				t.Fatal("lost immutable version exposed download")
			}
		})
	}
}

func TestNativeStageLogsExactIdentityAndAcceptedInputRefusals(t *testing.T) {
	for _, scenario := range []string{"container", "run-label", "stage-label", "name", "image-reference", "image-content", "actor", "job-version", "source", "legacy", "unsupported", "implicit-image", "receipt-missing", "foreign-subject"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeLogFixture(t)
			fields := []string{strings.Repeat("a", 64), "/web", "sha256:" + strings.Repeat("b", 64), "registry.invalid/web:v1", f.run.ID, "deploy-web", "running", "configured", "healthy"}
			subject := f.subject()
			switch scenario {
			case "container":
				fields[0] = strings.Repeat("c", 64)
			case "run-label":
				fields[4] = "foreign"
			case "stage-label":
				fields[5] = "foreign"
			case "name":
				fields[1] = "/foreign"
			case "image-reference":
				fields[3] = "registry.invalid/foreign:v1"
			case "image-content":
				fields[2] = "sha256:" + strings.Repeat("c", 64)
			case "actor":
				f.run.StartedByUserSub = "foreign"
			case "job-version":
				f.run.PipelineVersion++
			case "source":
				f.p.Source = &model.PipelineSourceConfig{ConnectionID: "unadmitted", Repository: "other/repo", FullRef: "refs/heads/main"}
			case "legacy":
				f.p.Version = 0
				f.run.PipelineVersion = 0
				f.planned.PipelineVersion = 0
			case "unsupported":
				f.p.Stages[0].Config.DeployRuntime = "compose"
			case "implicit-image":
				f.p.Stages[0].Config.Image = ""
			case "receipt-missing":
				f.run.StageRuns[0].RuntimeReceipt = nil
			case "foreign-subject":
				subject.RunID = "foreign"
			}
			f.persist(t)
			b, _ := json.Marshal(fields)
			if err := os.WriteFile(filepath.Join(f.dir, strings.Repeat("a", 64)+".json"), b, 0600); err != nil {
				t.Fatal(err)
			}
			page, err := f.r.OpenNativeStageLogSession(context.Background(), subject, f.jobs, nil, logSessionOptions())
			nativeLogNoBytes(t, page, err)
			f.noLogs(t)
		})
	}
}

func TestNativeStageLogsEditedDefinitionDoesNotRetargetAndDeletedRefuses(t *testing.T) {
	f := newNativeLogFixture(t)
	f.persist(t)
	p, err := f.r.Store.Pipelines.Get(context.Background(), f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Stages = []model.Stage{{ID: "replacement", Type: model.StageTypeApproval}}
	if err = f.r.Store.Pipelines.Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, nil, logSessionOptions())
	if err != nil || page.ContainerID != strings.Repeat("a", 64) {
		t.Fatal("current definition replaced accepted log identity", err)
	}
	if err = f.r.Store.Pipelines.Delete(context.Background(), f.p.ID); err != nil {
		t.Fatal(err)
	}
	paged, err := f.r.ReadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, nil, 0, 10)
	nativeLogNoBytes(t, paged, err)
	if _, _, err = f.r.PrepareNativeStageLogs(context.Background(), f.subject(), f.jobs, nil); err == nil {
		t.Fatal("deleted definition started producer admission")
	}
}

func TestNativeStageLogsIdentityDriftDuringCaptureDiscardsFirstPage(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	f.persist(t)
	hook := &nativeLogEnvHook{EnvironmentStore: f.resolver.Environments}
	hook.hook = func() {
		calls, _ := os.ReadFile(f.calls)
		if !strings.Contains(string(calls), "logs\n") {
			return
		}
		hook.fired = true
		run, err := f.r.Store.Runs.Get(context.Background(), f.run.ID)
		if err != nil {
			t.Fatal(err)
		}
		run.StageRuns[0].RuntimeReceipt.CapturedAt = run.StageRuns[0].RuntimeReceipt.CapturedAt.Add(time.Second)
		if err = f.r.Store.Runs.Update(context.Background(), run); err != nil {
			t.Fatal(err)
		}
	}
	f.resolver.Environments = hook
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
	nativeLogNoBytes(t, page, err)
	if len(f.r.runtimeLogCache().sessions) != 0 || !hook.fired {
		t.Fatal("identity recheck not reached or failed capture retained")
	}
}

func TestNativeStageLogsResolverReceiptDriftRefusesAtAdmission(t *testing.T) {
	for _, boundary := range []string{"capture", "page", "download", "live"} {
		t.Run(boundary, func(t *testing.T) {
			f := newNativeLogFixture(t)
			f.selectedDatabase(t)
			f.persist(t)
			var sessionID string
			if boundary == "page" || boundary == "download" {
				page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
				if err != nil {
					t.Fatal("initial capture failed", err)
				}
				sessionID = page.SessionID
			}
			before, _ := os.ReadFile(f.calls)
			hook := &nativeLogEnvHook{EnvironmentStore: f.resolver.Environments}
			hook.hook = func() {
				hook.fired = true
				run, err := f.r.Store.Runs.Get(context.Background(), f.run.ID)
				if err != nil {
					t.Fatal(err)
				}
				run.StageRuns[0].RuntimeReceipt.CapturedAt = run.StageRuns[0].RuntimeReceipt.CapturedAt.Add(time.Second)
				if err = f.r.Store.Runs.Update(context.Background(), run); err != nil {
					t.Fatal(err)
				}
			}
			f.resolver.Environments = hook
			switch boundary {
			case "capture":
				page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
				nativeLogNoBytes(t, page, err)
			case "page":
				page, err := f.r.ReadNativeStageLogSession(context.Background(), f.subject(), sessionID, f.jobs, f.resolver, 0, 10)
				nativeLogNoBytes(t, page, err)
			case "download":
				data, err := f.r.DownloadNativeStageLogSession(context.Background(), f.subject(), sessionID, f.jobs, f.resolver)
				if err == nil || data != "" {
					t.Fatal("receipt changed during resolution exposed a download")
				}
			case "live":
				channel, produce, err := f.r.PrepareNativeStageLogs(context.Background(), f.subject(), f.jobs, f.resolver)
				if err == nil || channel != "" || produce != nil {
					t.Fatal("receipt changed during resolution admitted a live producer")
				}
			}
			after, _ := os.ReadFile(f.calls)
			if !hook.fired || strings.Count(string(before), "logs\n") != strings.Count(string(after), "logs\n") {
				t.Fatal("receipt drift was not rejected before a new log read")
			}
		})
	}
}

func TestNativeStageLogsSearchAfterRedactionAndBounds(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	f.persist(t)
	opts := logSessionOptions()
	opts.Search = "selected-secret-canary"
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, opts)
	if err != nil || page.Total != 0 {
		t.Fatal("native search preceded redaction", err)
	}
	for _, bad := range []RuntimeLogOptions{{ContainerID: strings.Repeat("c", 64)}, {ExpectedRunID: "foreign"}, {MaxLines: -1}, {MaxLines: 10001}, {Search: strings.Repeat("x", 257)}, {Since: time.Now().Add(-25 * time.Hour), Until: time.Now()}} {
		p, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, bad)
		if !errors.Is(err, ErrRuntimeLogRequest) {
			t.Fatal("native bounds admitted")
		}
		nativeLogNoBytes(t, p, err)
	}
	for _, cursor := range [][2]int{{-1, 1}, {0, 501}, {1, 1}} {
		p, err := f.r.ReadNativeStageLogSession(context.Background(), f.subject(), page.SessionID, f.jobs, f.resolver, cursor[0], cursor[1])
		nativeLogNoBytes(t, p, err)
	}
}

func TestNativeStageLogsSplitStreamsPrivateErrorsAndCaptureCaps(t *testing.T) {
	for _, mode := range []string{"split", "fail", "overflow", "lines"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeLogFixture(t)
			f.selectedDatabase(t)
			f.persist(t)
			t.Setenv("COOKER_NATIVE_LOG_MODE", mode)
			page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
			if mode == "split" {
				if err != nil || page.Total != 2 || page.Entries[0].Message != "[REDACTED]" || page.Entries[1].Message != "ordinary-config" {
					t.Fatal("split stdout/stderr secret escaped", err)
				}
				return
			}
			nativeLogNoBytes(t, page, err)
			if len(f.r.runtimeLogCache().sessions) != 0 || f.r.runtimeLogCache().captures != 0 {
				t.Fatal("failed bounded capture retained bytes or reservation")
			}
		})
	}
}

func TestNativeStageLogsSnapshotCancellationAndDescendantPipeBound(t *testing.T) {
	for _, mode := range []string{"wait", "descendant"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeLogFixture(t)
			f.persist(t)
			t.Setenv("COOKER_NATIVE_LOG_MODE", mode)
			if mode == "descendant" {
				t.Cleanup(func() {
					_ = os.WriteFile(filepath.Join(f.dir, "release"), []byte("release"), 0600)
					deadline := time.Now().Add(3 * time.Second)
					for time.Now().Before(deadline) {
						if _, err := os.Stat(filepath.Join(f.dir, "done")); err == nil {
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
					t.Error("owned finite descendant did not finish")
				})
			}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			start := time.Now()
			page, err := f.r.OpenNativeStageLogSession(ctx, f.subject(), f.jobs, nil, logSessionOptions())
			nativeLogNoBytes(t, page, err)
			if time.Since(start) > 2*time.Second || len(f.r.runtimeLogCache().sessions) != 0 {
				t.Fatal("capture waited indefinitely or retained a failed session")
			}
		})
	}
}

func TestNativeStageLogsCloseUsesPresenceOnlyAndTypedSubject(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	f.persist(t)
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(f.calls)
	reads := len(f.manager.gets)
	nativeLogRotate(t, f)
	f.run.StageRuns[0].RuntimeReceipt = nil
	if err = f.r.Store.Runs.Update(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
	foreign := f.subject()
	foreign.StageID = "other-stage"
	if err = f.r.CloseNativeStageLogSession(context.Background(), foreign, page.SessionID); err != nil {
		t.Fatal(err)
	}
	if len(f.r.runtimeLogCache().sessions) != 1 {
		t.Fatal("cross-stage close removed entry")
	}
	if err = f.r.CloseNativeStageLogSession(context.Background(), f.subject(), page.SessionID); err != nil {
		t.Fatal("invalidated generation could not be freed", err)
	}
	if len(f.r.runtimeLogCache().sessions) != 0 || f.r.runtimeLogCache().bytes != 0 {
		t.Fatal("native close retained memory")
	}
	after, _ := os.ReadFile(f.calls)
	if string(before) != string(after) || len(f.manager.gets) != reads {
		t.Fatal("close unnecessarily inspected logs/runtime or resolved secrets")
	}
	if err = f.r.Store.Pipelines.Delete(context.Background(), f.p.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.r.CloseNativeStageLogSession(context.Background(), f.subject(), page.SessionID); !errors.Is(err, ErrRuntimeLogExpired) {
		t.Fatal("deleted native subject close did not report absence", err)
	}
}

func TestNativeStageLogsPreparedProducerConnectionLifetimeAndGeneration(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	f.persist(t)
	admission, cancelAdmission := context.WithCancel(context.Background())
	channel, produce, err := f.r.PrepareNativeStageLogs(admission, f.subject(), f.jobs, f.resolver)
	if err != nil || produce == nil || !strings.HasPrefix(channel, "native-runtime-logs:") {
		t.Fatal("private producer was not prepared", err)
	}
	cancelAdmission()
	t.Setenv("COOKER_NATIVE_LOG_MODE", "split")
	var out bytes.Buffer
	if err = produce(context.Background(), &out); err != nil || strings.Contains(out.String(), "canary") || !strings.Contains(out.String(), "[REDACTED]") {
		t.Fatal("producer inherited admission cancellation or skipped redaction", err)
	}
	nativeLogRotate(t, f)
	out.Reset()
	callsBefore, _ := os.ReadFile(f.calls)
	if err = produce(context.Background(), &out); !errors.Is(err, ErrRuntimeLogUnavailable) || out.Len() != 0 {
		t.Fatal("generation changed before command but producer emitted bytes", err)
	}
	callsAfter, _ := os.ReadFile(f.calls)
	if strings.Count(string(callsAfter), "logs\n") != strings.Count(string(callsBefore), "logs\n") {
		t.Fatal("refused producer still started log command")
	}
}

func TestNativeStageLogsProducerOwnsCancellationAndRequestValues(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	f.persist(t)
	_, produce, err := f.r.PrepareNativeStageLogs(context.Background(), f.subject(), f.jobs, f.resolver)
	if err != nil {
		t.Fatal(err)
	}
	type requestKey struct{}
	f.manager.onGet = func(ctx context.Context, _ string) {
		if ctx.Value(requestKey{}) != "request-value" {
			t.Error("producer lost connection request values")
		}
	}
	t.Setenv("COOKER_NATIVE_LOG_MODE", "wait")
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), requestKey{}, "request-value"), 200*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	start := time.Now()
	err = produce(ctx, &out)
	if !errors.Is(err, ErrRuntimeLogUnavailable) || time.Since(start) > 2*time.Second || ctx.Err() != context.DeadlineExceeded || !strings.Contains(out.String(), "stream-marker") {
		t.Fatal("live follow did not use connection lifetime", err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "live-started")); err != nil {
		t.Fatal("owned live CLI did not start", err)
	}
}

func TestNativeStageLogsChannelTupleEncodingIsInjective(t *testing.T) {
	for _, pair := range [][2]NativeRuntimeSubject{
		{{Kind: "native-stage", PipelineID: "a:b", RunID: "c", StageID: "d"}, {Kind: "native-stage", PipelineID: "a", RunID: "b:c", StageID: "d"}},
		{{Kind: "native-stage", PipelineID: "p%41", RunID: "รัน", StageID: "stage:1"}, {Kind: "native-stage", PipelineID: "pA", RunID: "รัน", StageID: "stage:1"}},
	} {
		a, b := nativeRuntimeLogChannel(pair[0], strings.Repeat("a", 64)), nativeRuntimeLogChannel(pair[1], strings.Repeat("a", 64))
		if a == b {
			t.Fatal("opaque native identity tuple collided")
		}
		for i, channel := range []string{a, b} {
			data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(channel, "native-runtime-logs:"))
			if err != nil {
				t.Fatal(err)
			}
			var tuple []string
			if json.Unmarshal(data, &tuple) != nil || !reflect.DeepEqual(tuple, []string{"native-stage", pair[i].PipelineID, pair[i].RunID, pair[i].StageID, strings.Repeat("a", 64)}) {
				t.Fatal("channel did not encode exact safe tuple")
			}
		}
	}
}
