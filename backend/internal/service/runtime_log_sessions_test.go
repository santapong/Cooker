package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
)

func logSessionCLI(t *testing.T, label, project, name, logs string, two bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin, calls := filepath.Join(dir, "docker"), filepath.Join(dir, "calls")
	first, second := strings.Repeat("a", 64), strings.Repeat("b", 64)
	ids := first
	if two {
		ids += "\n" + second
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + calls + "'\ncase \"$1\" in\nps) cat <<'IDS'\n" + ids + "\nIDS\n;;\ninspect) for last; do :; done; if test \"$last\" = '" + second + "'; then id='" + second + "'; name='original-web-2'; else id='" + first + "'; name='" + name + "'; fi; printf '%s\\n' \"$id|" + label + "|/$name|" + project + "|web|False|running\";;\nlogs) cat <<'LOGS'\n" + logs + "\nLOGS\n;;\n*) exit 1;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return bin, calls
}

func TestRuntimeLogMixedAppNativeTypedOwnership(t *testing.T) {
	f := newNativeLogFixture(t)
	f.persist(t)
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, nil, logSessionOptions())
	if err != nil {
		t.Fatal(err)
	}
	c := f.r.runtimeLogCache()
	// Cache-only App fixture deliberately reuses the native pipeline/stage IDs.
	// Existing App capture/rotation regressions still exercise the real wrappers.
	appSubject := appRuntimeLogSubject(f.p.ID, "deploy-web")
	app := &runtimeLogSnapshot{subject: appSubject, page: RuntimeLogPage{AppID: f.p.ID, Service: "deploy-web", RunID: f.run.ID}, entries: []RuntimeLogEntry{{Sequence: 1, Message: "safe-app"}}, bytes: 100}
	if err = c.save(app); err != nil {
		t.Fatal(err)
	}
	nativeSubject := nativeRuntimeLogSubject(f.subject())
	if _, err = c.load(appSubject, page.SessionID); !errors.Is(err, ErrRuntimeLogExpired) {
		t.Fatal("App read acquired native session")
	}
	if _, err = c.load(nativeSubject, app.page.SessionID); !errors.Is(err, ErrRuntimeLogExpired) {
		t.Fatal("native read acquired App session")
	}
	f.r.CloseRuntimeLogSession(f.p.ID, "deploy-web", page.SessionID)
	if _, err = c.load(nativeSubject, page.SessionID); err != nil {
		t.Fatal("App close removed native session")
	}
	if err = f.r.CloseNativeStageLogSession(context.Background(), f.subject(), app.page.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = c.load(appSubject, app.page.SessionID); err != nil {
		t.Fatal("native close removed App session")
	}
	wrong := nativeSubject
	wrong.stageID = "other-stage"
	if _, err = c.load(wrong, page.SessionID); !errors.Is(err, ErrRuntimeLogExpired) {
		t.Fatal("cross-stage cached read admitted")
	}
	mixed := nativeSubject
	mixed.appID = f.p.ID
	if _, err = c.load(mixed, page.SessionID); !errors.Is(err, ErrRuntimeLogExpired) {
		t.Fatal("mixed subject fields admitted")
	}
	c.close(mixed, page.SessionID)
	if _, err = c.load(nativeSubject, page.SessionID); err != nil {
		t.Fatal("mixed subject removed native session")
	}
	if err = f.r.CloseNativeStageLogSession(context.Background(), f.subject(), page.SessionID); err != nil {
		t.Fatal(err)
	}
	f.r.CloseRuntimeLogSession(f.p.ID, "deploy-web", app.page.SessionID)
	if c.bytes != 0 || len(c.sessions) != 0 {
		t.Fatal("typed closes did not release shared capacity")
	}
}

func TestRuntimeLogMixedAppNativeSharedCaptureAndCountBudgets(t *testing.T) {
	f := newNativeLogFixture(t)
	f.persist(t)
	c := f.r.runtimeLogCache()
	for i := 0; i < 4; i++ {
		if err := c.reserveCapture(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, nil, logSessionOptions()); !errors.Is(err, ErrRuntimeLogCapacity) {
		t.Fatal("native capture bypassed four shared reservations")
	}
	f.noLogs(t)
	if err := c.reserveCapture(); !errors.Is(err, ErrRuntimeLogCapacity) {
		t.Fatal("fifth mixed capture reservation admitted")
	}
	for i := 0; i < 4; i++ {
		c.releaseCapture()
	}
	c.maxCount = 1
	app := &runtimeLogSnapshot{subject: appRuntimeLogSubject("app", "web"), page: RuntimeLogPage{AppID: "app", Service: "web"}, bytes: 100}
	if err := c.save(app); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, nil, logSessionOptions()); !errors.Is(err, ErrRuntimeLogCapacity) {
		t.Fatal("native session bypassed App occupancy")
	}
	c.close(app.subject, app.page.SessionID)
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, nil, logSessionOptions())
	if err != nil {
		t.Fatal(err)
	}
	other := &runtimeLogSnapshot{subject: appRuntimeLogSubject("app", "web"), page: RuntimeLogPage{AppID: "app", Service: "web"}, bytes: 100}
	if err = c.save(other); !errors.Is(err, ErrRuntimeLogCapacity) {
		t.Fatal("App session bypassed native occupancy")
	}
	if c.captures != 0 {
		t.Fatal("native capture reservation leaked")
	}
	if err = f.r.CloseNativeStageLogSession(context.Background(), f.subject(), page.SessionID); err != nil {
		t.Fatal(err)
	}
	c.maxCount = 128
	c.maxBytes = 50
	if err = c.save(other); !errors.Is(err, ErrRuntimeLogCapacity) {
		t.Fatal("shared logical byte budget bypassed")
	}
}

func TestRuntimeLogNativeMetadataAccountingCopiesAndExpiry(t *testing.T) {
	f := newNativeLogFixture(t)
	f.selectedDatabase(t)
	ref := secrets.VersionReference{SchemaVersion: 1, Provider: "vault", Resource: strings.Repeat("r", 900), Version: "1", Scope: secrets.VersionScope{Authority: "https://vault.fixture.invalid", EnvironmentID: "accepted-env", Key: "TOKEN"}}
	pin := f.payload.Secrets.Environments["accepted-env"]
	pin.Mode = "external"
	pin.References = map[string]secrets.VersionReference{"TOKEN": ref}
	f.resolver.Secrets = &nativeLogExternalSecrets{refs: map[string]secrets.VersionReference{"TOKEN": ref}, values: map[string][]byte{"TOKEN": []byte("selected-secret-canary")}}
	f.persist(t)
	c := f.r.runtimeLogCache()
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	page, err := f.r.OpenNativeStageLogSession(context.Background(), f.subject(), f.jobs, f.resolver, logSessionOptions())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := c.load(nativeRuntimeLogSubject(f.subject()), page.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(snapshot.native)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.bytes < len(metadata)+len(snapshot.entries)*128+1024 || c.bytes != snapshot.bytes || len(metadata) <= 1024 || strings.Contains(string(metadata), "selected-secret-canary") || strings.Contains(string(metadata), "unselected-secret-canary") {
		t.Fatal("variable native metadata was uncharged or retained credentials")
	}
	ref.Resource = "caller-changed"
	pin.References["TOKEN"] = ref
	pin.Keys[0] = "OTHER"
	if snapshot.native.Generation.References["TOKEN"].Resource == "caller-changed" || snapshot.native.Generation.Keys[0] != "TOKEN" {
		t.Fatal("cache retained mutable accepted metadata")
	}
	app := &runtimeLogSnapshot{subject: appRuntimeLogSubject("app", "web"), page: RuntimeLogPage{AppID: "app", Service: "web"}, bytes: 100}
	if err = c.save(app); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Minute)
	if _, err = c.load(nativeRuntimeLogSubject(f.subject()), page.SessionID); !errors.Is(err, ErrRuntimeLogExpired) {
		t.Fatal("expired native entry survived")
	}
	if c.bytes != 0 || len(c.sessions) != 0 {
		t.Fatal("mixed expiry failed exact logical-byte release")
	}
}

func logSessionOptions() RuntimeLogOptions {
	return RuntimeLogOptions{Since: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Until: time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)}
}

func TestRuntimeLogSnapshotReplicaScopeRedactionAndStablePaging(t *testing.T) {
	r, app, jobs, resolver, history := runtimeSessionFixture(t, "run")
	r.DockerBin, _ = logSessionCLI(t, "run", "original", "original-web-1", "2026-10-01T00:01:00Z old-secret-value\n2026-10-01T00:02:00Z second\n2026-10-01T00:03:00Z third", true)
	replicas, err := r.DeploymentLogReplicas(context.Background(), app, "web")
	if err != nil || len(replicas.Replicas) != 2 || replicas.RunID != "run" {
		t.Fatal("replica inventory unavailable")
	}
	if _, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, logSessionOptions()); !errors.Is(err, ErrRuntimeLogRequest) {
		t.Fatal("multiple replicas selected implicitly")
	}
	opts := logSessionOptions()
	opts.ContainerID = replicas.Replicas[1].ContainerID
	page, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, opts)
	if err != nil || page.ContainerID != strings.Repeat("b", 64) || page.Total != 3 || page.Entries[0].Message != "[REDACTED]" {
		t.Fatal("snapshot identity or redaction failed")
	}
	if history.calls != 3 {
		t.Fatal("history was not captured once for each discovery/open")
	}
	// A new deployment cannot retarget this cached session or reorder a reconnect.
	if err := r.Store.AppDeploys.Create(context.Background(), &model.AppDeploy{ID: "replacement", AppID: app.ID, RunID: "new-run", PipelineID: "new-pipeline", Status: model.RunStatusSuccess, CreatedAt: time.Now().Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	p1, err := r.ReadRuntimeLogSession(context.Background(), app.ID, "web", page.SessionID, jobs, resolver, 1, 1)
	if err != nil || p1.RunID != "run" || len(p1.Entries) != 1 || p1.Entries[0].Sequence != 2 || p1.NextOffset == nil || *p1.NextOffset != 2 {
		t.Fatal("stable reconnect page failed")
	}
	p2, err := r.ReadRuntimeLogSession(context.Background(), app.ID, "web", page.SessionID, jobs, resolver, 1, 1)
	if err != nil || !reflect.DeepEqual(p1, p2) {
		t.Fatal("repeated cursor changed")
	}
	p1.Entries[0].Message = "caller edit"
	download, err := r.DownloadRuntimeLogSession(context.Background(), app.ID, "web", page.SessionID, jobs, resolver)
	if err != nil || strings.Contains(download, "old-secret-value") || strings.Contains(download, "caller edit") || !strings.Contains(download, "[REDACTED]") {
		t.Fatal("download escaped redaction or snapshot immutability")
	}
	if history.calls != 3 {
		t.Fatal("retained page followed mutable history")
	}
	if _, err := r.ReadRuntimeLogSession(context.Background(), "another-app", "web", page.SessionID, jobs, resolver, 0, 10); !errors.Is(err, ErrRuntimeLogExpired) {
		t.Fatal("cross-app session accepted")
	}
	if _, err := r.ReadRuntimeLogSession(context.Background(), app.ID, "another-service", page.SessionID, jobs, resolver, 0, 10); !errors.Is(err, ErrRuntimeLogExpired) {
		t.Fatal("cross-service session accepted")
	}
}

func TestRuntimeLogSnapshotRejectsUnownedReplacedAndRotatedInput(t *testing.T) {
	for _, tc := range []struct{ label, project, name string }{{"other-run", "original", "original-web-1"}, {"run", "another-project", "original-web-1"}, {"run", "original", "unrelated"}} {
		t.Run(tc.label+tc.project+tc.name, func(t *testing.T) {
			r, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
			bin, calls := logSessionCLI(t, tc.label, tc.project, tc.name, "private fixture output", false)
			r.DockerBin = bin
			if _, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, logSessionOptions()); !errors.Is(err, ErrRuntimeLogUnavailable) {
				t.Fatal("unverified replica admitted")
			}
			data, _ := os.ReadFile(calls)
			if strings.Contains(string(data), "logs --") {
				t.Fatal("logs opened before identity check")
			}
		})
	}
	r, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
	r.DockerBin, _ = logSessionCLI(t, "run", "original", "original-web-1", "old-secret-value", false)
	page, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, logSessionOptions())
	if err != nil {
		t.Fatal(err)
	}
	env, _ := resolver.Environments.Get(context.Background(), "original")
	env.PlainVars = map[string]string{"CHANGED": "yes"}
	if err := resolver.Environments.Update(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadRuntimeLogSession(context.Background(), app.ID, "web", page.SessionID, jobs, resolver, 0, 10); !errors.Is(err, ErrRuntimeLogUnavailable) {
		t.Fatal("rotated configuration replayed cached logs")
	}
	if data, err := r.DownloadRuntimeLogSession(context.Background(), app.ID, "web", page.SessionID, jobs, resolver); !errors.Is(err, ErrRuntimeLogUnavailable) || data != "" {
		t.Fatal("rotated download exposed bytes")
	}
}

func TestRuntimeLogSnapshotSearchRunsAfterRedactionAndHonorsBounds(t *testing.T) {
	r, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
	bin, calls := logSessionCLI(t, "run", "original", "original-web-1", "2026-10-01T00:01:00Z old-secret-value\n2026-10-01T00:02:00Z public config", false)
	r.DockerBin = bin
	opts := logSessionOptions()
	opts.Search = "old-secret-value"
	page, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, opts)
	if err != nil || page.Total != 0 {
		t.Fatal("search used unredacted output")
	}
	data, _ := os.ReadFile(calls)
	for _, arg := range []string{"--timestamps --tail 2000", "--since 2026-10-01T00:00:00Z", "--until 2026-10-01T01:00:00Z", strings.Repeat("a", 64)} {
		if !strings.Contains(string(data), arg) {
			t.Fatal("bounded Docker read argument missing")
		}
	}
	for _, bad := range []RuntimeLogOptions{{MaxLines: -1}, {MaxLines: 10001}, {ContainerID: "other"}, {Search: strings.Repeat("x", 257)}, {Since: time.Now(), Until: time.Now().Add(-time.Hour)}, {Since: time.Now().Add(-48 * time.Hour), Until: time.Now()}} {
		if _, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, bad); !errors.Is(err, ErrRuntimeLogRequest) {
			t.Fatal("invalid bounds admitted")
		}
	}
	if _, err := r.ReadRuntimeLogSession(context.Background(), app.ID, "web", page.SessionID, jobs, resolver, -1, 10); !errors.Is(err, ErrRuntimeLogRequest) {
		t.Fatal("negative cursor admitted")
	}
	if _, err := r.ReadRuntimeLogSession(context.Background(), app.ID, "web", page.SessionID, jobs, resolver, 0, 501); !errors.Is(err, ErrRuntimeLogRequest) {
		t.Fatal("unbounded page admitted")
	}
	// A Reader without WriterTo must not bypass the byte cap through ReadFrom.
	b := &boundedRuntimeLogBuffer{limit: 16}
	_, err = io.Copy(b, struct{ io.Reader }{strings.NewReader(strings.Repeat("x", 1024))})
	if err == nil || b.Len() > 16 {
		t.Fatal("CLI output bypassed byte cap")
	}
}

func TestRuntimeLogSnapshotExpiryDeletionAndCapacity(t *testing.T) {
	r, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
	r.DockerBin, _ = logSessionCLI(t, "run", "original", "original-web-1", "old-secret-value", false)
	c := r.runtimeLogCache()
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	c.maxCount = 1
	page, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, logSessionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, logSessionOptions()); !errors.Is(err, ErrRuntimeLogCapacity) {
		t.Fatal("session cap ignored")
	}
	r.CloseRuntimeLogSession("another-app", "web", page.SessionID)
	if _, err := r.ReadRuntimeLogSession(context.Background(), app.ID, "web", page.SessionID, jobs, resolver, 0, 10); err != nil {
		t.Fatal("unowned close removed session")
	}
	now = now.Add(11 * time.Minute)
	if _, err := r.ReadRuntimeLogSession(context.Background(), app.ID, "web", page.SessionID, jobs, resolver, 0, 10); !errors.Is(err, ErrRuntimeLogExpired) {
		t.Fatal("expired cursor silently reopened")
	}
	page, err = r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, logSessionOptions())
	if err != nil {
		t.Fatal(err)
	}
	r.CloseRuntimeLogSession(app.ID, "web", page.SessionID)
	if c.bytes != 0 || len(c.sessions) != 0 {
		t.Fatal("close retained log memory")
	}
	c.maxBytes = 1
	if _, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, logSessionOptions()); !errors.Is(err, ErrRuntimeLogCapacity) {
		t.Fatal("global byte budget ignored")
	}
}

func TestRuntimeLogSnapshotRotationDuringCapturePublishesNothing(t *testing.T) {
	r, app, jobs, resolver, _ := runtimeSessionFixture(t, "run")
	bin, calls := logSessionCLI(t, "run", "original", "original-web-1", "old-secret-value", false)
	r.DockerBin = bin
	original := resolver.Environments
	resolver.Environments = &runtimeLogCaptureRotation{EnvironmentStore: original, calls: calls}
	if page, err := r.OpenRuntimeLogSession(context.Background(), app, "web", jobs, resolver, logSessionOptions()); !errors.Is(err, ErrRuntimeLogUnavailable) || len(page.Entries) != 0 || page.SessionID != "" {
		t.Fatal("rotation during capture published a first page")
	}
	data, _ := os.ReadFile(calls)
	if !strings.Contains(string(data), "logs --timestamps") || len(r.runtimeLogCache().sessions) != 0 {
		t.Fatal("rotation boundary was not reached or left cached data")
	}
}

type runtimeLogCaptureRotation struct {
	store.EnvironmentStore
	calls   string
	rotated bool
}

func (s *runtimeLogCaptureRotation) Get(ctx context.Context, id string) (*model.Environment, error) {
	data, _ := os.ReadFile(s.calls)
	if !s.rotated && strings.Contains(string(data), "logs --timestamps") {
		env, err := s.EnvironmentStore.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		env.PlainVars = map[string]string{"ROTATED": "yes"}
		if err := s.EnvironmentStore.Update(ctx, env); err != nil {
			return nil, err
		}
		s.rotated = true
	}
	return s.EnvironmentStore.Get(ctx, id)
}
