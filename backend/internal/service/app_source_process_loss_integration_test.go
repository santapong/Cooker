//go:build integration

package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/pusher"
	cookercrypto "github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// The controlled harness creates only labelled, internal, cached-input resources.
// This is the manual Dockerfile App boundary AFTER real terminal result writes,
// BEFORE the real PostgreSQL Complete call. It does not prove hosted Git, private
// FROM, other interruptions, automatic reconciliation, or exactly-once effects.
func TestAppSourceAfterTerminalProcessLoss(t *testing.T) {
	if os.Getenv("COOKER_APP_PROCESS_LOSS_ACCEPTANCE") != "owned-local-v1" {
		t.Skip("explicit owned manual App process-loss harness required")
	}
	nonce := os.Getenv("COOKER_APP_RUNTIME_NONCE")
	if u, e := uuid.Parse(nonce); e != nil || u.String() != nonce || u == uuid.Nil {
		t.Fatal("invalid owned process-loss nonce")
	}
	network := "cooker-app-runtime-" + nonce + "-net"
	if os.Getenv("COOKER_APP_RUNTIME_NETWORK") != network {
		t.Fatal("unowned process-loss network")
	}
	ctx, stop := context.WithTimeout(context.Background(), 4*time.Minute)
	defer stop()
	if nativeRuntimeCommand(ctx, t, "docker", "network", "inspect", "--format", `{{index .Labels "cooker.fixture"}}`, network) != nonce || nativeRuntimeCommand(ctx, t, "docker", "network", "inspect", "--format", `{{.Internal}}`, network) != "true" {
		t.Fatal("unowned or externally routed fixture")
	}
	bk, e := url.Parse(os.Getenv("COOKER_APP_RUNTIME_BUILDKIT"))
	if e != nil || bk.Scheme != "tcp" || bk.Hostname() != "127.0.0.1" || bk.Port() == "" || bk.User != nil || bk.Path != "" || bk.RawQuery != "" {
		t.Fatal("unowned BuildKit endpoint")
	}
	dsn, e := appRuntimeOwnedDatabaseURL(os.Getenv("COOKER_APP_RUNTIME_DATABASE_URL"))
	if e != nil {
		t.Fatal("unowned process-loss database")
	}
	admin, e := sql.Open("postgres", dsn.String())
	if e != nil {
		t.Fatal("owned database unavailable")
	}
	defer admin.Close()
	schema := "app_loss_" + strings.ReplaceAll(nonce, "-", "")
	if _, e := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal("owned schema unavailable")
	}
	defer func() {
		if _, e := admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error("owned schema cleanup failed")
		}
	}()
	params := dsn.Query()
	params.Set("search_path", schema)
	dsn.RawQuery = params.Encode()
	st, e := storepg.NewStore(ctx, dsn.String())
	if e != nil {
		t.Fatal("owned store unavailable")
	}
	defer st.Close()
	db, e := sql.Open("postgres", dsn.String())
	if e != nil {
		t.Fatal("owned queue unavailable")
	}
	defer db.Close()
	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	if os.Mkdir(repository, 0700) != nil {
		t.Fatal("owned source directory unavailable")
	}
	server, e := os.ReadFile(os.Getenv("COOKER_APP_RUNTIME_SERVER_BINARY"))
	if e != nil || len(server) == 0 {
		t.Fatal("owned server input unavailable")
	}
	marker := "accepted-app-loss-" + nonce
	for name, data := range map[string][]byte{"server": server, "message.txt": []byte(marker), ".dockerignore": []byte(".git\n"), "Dockerfile": []byte("FROM scratch\nCOPY server /server\nCOPY message.txt /message.txt\nRUN [\"/server\",\"build-proof\"]\nHEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=15 CMD [\"/server\",\"probe\"]\nENTRYPOINT [\"/server\"]\n")} {
		if os.WriteFile(filepath.Join(repository, name), data, 0700) != nil {
			t.Fatal("owned source input unavailable")
		}
	}
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "init", "--initial-branch=main")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "add", "Dockerfile", "server", "message.txt", ".dockerignore")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "-c", "user.name=Owned fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "owned manual App process-loss source")
	commit := nativeRuntimeCommand(ctx, t, "git", "-C", repository, "rev-parse", "HEAD")
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		t.Fatal("owned key unavailable")
	}
	codec, e := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(key))
	if e != nil {
		t.Fatal("owned codec unavailable")
	}
	password, appCanary, sourceCanary := "loss-registry-"+uuid.NewString(), "loss-log-"+uuid.NewString(), "loss-source-"+uuid.NewString()
	username := "owned-loss-fixture"
	var auth, uploads, manifests atomic.Int32
	reg := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	regServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != username || p != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="owned-app-loss"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		auth.Add(1)
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/manifests/") {
			uploads.Add(1)
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/manifests/") {
			manifests.Add(1)
		}
		reg.ServeHTTP(w, r)
	}))
	defer regServer.Close()
	host := strings.TrimPrefix(regServer.URL, "http://")
	workload := "cooker-app-" + strings.ReplaceAll(nonce, "-", "")[:24]
	imagePrefix := host + "/owned/" + workload
	if nativeRuntimeExists(ctx, "container", workload) {
		t.Fatal("owned workload name exists")
	}
	var image string
	defer func() { appRuntimeCleanup(t, nonce, workload, imagePrefix, image) }()
	connections := &connection.Service{Store: &connection.Postgres{DB: db}, Codec: codec}
	c := &connection.Connection{Name: "Owned process-loss source", Provider: "github", Enabled: true, Repositories: []string{"owned/app-runtime"}}
	if connections.Create(ctx, c, sourceCanary) != nil {
		t.Fatal("owned source connection unavailable")
	}
	manager := database.New(storepg.NewEnvironmentStore(db), codec)
	envID, incarnation := uuid.NewString(), uuid.NewString()
	regEnv := "registry_" + strings.ReplaceAll(incarnation, "-", "")
	for id, name := range map[string]string{envID: "Owned process-loss workload", regEnv: "Owned process-loss private registry"} {
		if st.Environments.Create(ctx, &model.Environment{ID: id, Name: name, PlainVars: map[string]string{}, CreatedAt: time.Now().UTC()}) != nil {
			t.Fatal("owned environment unavailable")
		}
	}
	if manager.Put(ctx, envID, "APP_LOG_CANARY", []byte(appCanary)) != nil || manager.Put(ctx, regEnv, "password", []byte(password)) != nil {
		t.Fatal("owned encrypted fixture values unavailable")
	}
	app := &model.App{ID: uuid.NewString(), Name: workload, GitHubRepo: "owned/app-runtime", Branch: "main", EnvironmentID: envID, RegistryRef: host + "/owned", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: workload}, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile", SourceConnectionID: c.ID, SourceConnectionVersion: c.Version, SourceRepositoryID: "owned-local-loss-" + nonce}}
	rc := &model.RegistryConfig{ID: uuid.NewString(), Name: "Owned loss registry", URL: regServer.URL, Username: username, PasswordRef: "registry:v2:" + incarnation, AllowedAppIDs: []string{app.ID}}
	if st.Registries.Create(ctx, rc) != nil {
		t.Fatal("owned registry unavailable")
	}
	app.BuildPlan.RegistryConnectionIDs = []string{rc.ID}
	if st.Apps.Create(ctx, app) != nil {
		t.Fatal("owned App unavailable")
	}
	app, e = st.Apps.Get(ctx, app.ID)
	if e != nil {
		t.Fatal("owned App read unavailable")
	}
	input := appLossInput{DSN: dsn.String(), Nonce: nonce, Schema: schema, ParentPID: os.Getpid(), Key: key, Repository: repository, Base: base, Commit: commit, Marker: marker, BuildKit: bk.String(), Network: network, Workload: workload, ImagePrefix: imagePrefix, AppCanary: appCanary, ConnectionID: c.ID, ConnectionVersion: c.Version, RepositoryID: app.BuildPlan.SourceRepositoryID, IntentID: "owned-manual-loss-" + nonce}
	producer, jobs, _ := appLossQueue(t, ctx, input, st, db, codec, false, nil, nil)
	receipt, e := producer.EnqueueIntent(ctx, app, "", &model.PipelineRun{StartedByUserSub: "owned-loss-admin", StartedByEmail: "fixture@example.invalid"}, input.IntentID)
	if e != nil {
		t.Fatal("owned manual admission failed")
	}
	input.JobID, input.RunID, input.PipelineID = receipt.JobID, receipt.RunID, receipt.PipelineID
	accepted, e := jobs.Get(ctx, receipt.JobID)
	if e != nil || accepted.Status != jobqueue.StatusPending || accepted.Attempts != 0 {
		t.Fatal("accepted manual job unavailable")
	}
	digest, e := jobqueue.PayloadDigest(accepted.Payload)
	if e != nil || digest != receipt.PlanDigest {
		t.Fatal("accepted payload digest differs")
	}
	input.PayloadDigest = digest
	var plan AppDeployPlan
	if json.Unmarshal(accepted.Payload, &plan) != nil || plan.Registry == nil || plan.Secrets == nil || plan.Images == nil || plan.Registry.Subject != (model.RegistrySubject{Kind: "app", ID: app.ID}) || plan.Commit != commit {
		t.Fatal("manual App accepted evidence unavailable")
	}
	// A moved ref must never replace the accepted checkout after admission.
	if os.WriteFile(filepath.Join(repository, "message.txt"), []byte("later-branch-must-not-run"), 0700) != nil {
		t.Fatal("owned source edit failed")
	}
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "add", "message.txt")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "-c", "user.name=Owned fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "move owned branch after admission")
	input.Phase = "worker"
	first := appLossStart(t, input)
	defer first.cleanup()
	pushed := first.event(t, ctx, "pushed")
	if pushed.PID == os.Getpid() || pushed.ParentPID != os.Getpid() || !strings.HasPrefix(pushed.Digest, "sha256:") || uploads.Load() == 0 {
		t.Fatal("new worker did not perform real authenticated OCI upload")
	}
	pushSnapshot := map[string]int32{"authenticated": auth.Load(), "uploads": uploads.Load(), "manifestReads": manifests.Load()}
	first.command(t, "deploy")
	barrier := first.event(t, ctx, "before-complete")
	if barrier.PID != first.cmd.Process.Pid || barrier.ParentPID != os.Getpid() || barrier.JobID != receipt.JobID || barrier.Owner == "" || barrier.Attempts != 1 || barrier.Digest != pushed.Digest || barrier.Effects["handler"] != 1 || barrier.Effects["build"] != 1 || barrier.Effects["push"] != 1 || barrier.Effects["deploy"] != 1 || barrier.Effects["checkout"] != 1 {
		t.Fatal("actual claim/effect barrier unavailable")
	}
	image = barrier.Image
	run, e := st.Runs.Get(ctx, receipt.RunID)
	history, h := st.AppDeploys.ListByApp(ctx, app.ID, 100)
	if e != nil || h != nil || len(history) != 1 || run.Status != model.RunStatusSuccess || run.Source != nil || run.StartedAt == nil || run.FinishedAt == nil || history[0].Status != model.RunStatusSuccess || history[0].Kind != model.AppDeployKindDeploy || history[0].RunID != run.ID || history[0].PipelineID != receipt.PipelineID || history[0].ImageRef != image || history[0].Digest != pushed.Digest {
		t.Fatal("real terminal result was not persisted before queue Complete")
	}
	for _, s := range run.StageRuns {
		if s.Status != model.RunStatusSuccess || s.StartedAt == nil || s.FinishedAt == nil {
			t.Fatal("real App stages did not finish")
		}
		if s.StageID == "build" && s.Outputs["digest"] != pushed.Digest {
			t.Fatal("real OCI output differs from pushed digest")
		}
	}
	for nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.State.Health.Status}}`, workload) != "healthy" {
		select {
		case <-ctx.Done():
			t.Fatal("owned runtime health deadline")
		case <-time.After(100 * time.Millisecond):
		}
	}
	containerID := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, workload)
	if nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", "{{index .Config.Labels \""+RuntimeRunLabel+"\"}}", workload) != run.ID || nativeRuntimeCommand(ctx, t, "docker", "exec", workload, "/server", "probe") != marker || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Config.Image}}`, workload) != image || !strings.HasSuffix(image, "@"+pushed.Digest) || manifests.Load() <= pushSnapshot["manifestReads"] {
		t.Fatal("real run-labelled digest/runtime marker evidence differs")
	}
	for _, path := range append(barrier.Checkouts, barrier.Artifacts...) {
		if _, e := os.Stat(path); !os.IsNotExist(e) {
			t.Fatal("worker terminal cleanup did not remove private checkout/artifact")
		}
	}
	live, e := jobs.Get(ctx, receipt.JobID)
	if e != nil || live.Status != jobqueue.StatusRunning || live.Attempts != 1 || live.LockedBy != barrier.Owner {
		t.Fatal("barrier did not retain actual running claim")
	}
	first.kill(t)
	// The second admission is real, distinct, and shares the same App concurrency key.
	second, e := producer.EnqueueIntent(ctx, app, commit, nil, input.IntentID+"-second")
	if e != nil || second.JobID == receipt.JobID {
		t.Fatal("same-key second admission failed")
	}
	secondJob, e := jobs.Get(ctx, second.JobID)
	if e != nil || secondJob.Status != jobqueue.StatusPending || secondJob.Attempts != 0 || secondJob.ConcurrencyKey != live.ConcurrencyKey {
		t.Fatal("same-key job is not pending")
	}
	beforeRecovery := map[string]int32{"authenticated": auth.Load(), "uploads": uploads.Load(), "manifestReads": manifests.Load()}
	input.Phase = "recovery"
	input.OriginalOwner = barrier.Owner
	input.SecondJobID = second.JobID
	fresh := appLossStart(t, input)
	defer fresh.cleanup()
	recovered := fresh.event(t, ctx, "recovered")
	if recovered.PID == barrier.PID || recovered.PID == os.Getpid() || recovered.ParentPID != os.Getpid() || !recovered.Fences || !recovered.ResolutionRollback || recovered.Attempts != 1 {
		t.Fatal("different-process recovery/fence proof unavailable")
	}
	for _, n := range recovered.Effects {
		if n != 0 {
			t.Fatal("fresh recovery attempted an execution/preparation effect")
		}
	}
	attention, e := jobs.Get(ctx, receipt.JobID)
	attentionRun, re := st.Runs.Get(ctx, run.ID)
	attentionHistory, he := st.AppDeploys.ListByApp(ctx, app.ID, 100)
	if e != nil || re != nil || he != nil || attention.Status != jobqueue.StatusNeedsAttention || attentionRun.Status != model.RunStatusNeedsAttention || attentionRun.FinishedAt != nil || len(attentionHistory) != 1 || attentionHistory[0].Status != model.RunStatusNeedsAttention {
		t.Fatal("lost acknowledgement was not labelled honestly")
	}
	appLossPreserved(t, run, history[0], attentionRun, attentionHistory[0])
	blocked, e := jobs.Get(ctx, second.JobID)
	if e != nil || blocked.Status != jobqueue.StatusPending || blocked.Attempts != 0 {
		t.Fatal("same-key second job escaped attention barrier")
	}
	// Inspection is an independent parent observation after recovery, before
	// authorizing the recorded outcome. No workload mutation is performed.
	if nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, workload) != containerID || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Config.Image}}`, workload) != image || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", "{{index .Config.Labels \""+RuntimeRunLabel+"\"}}", workload) != run.ID || nativeRuntimeCommand(ctx, t, "docker", "exec", workload, "/server", "probe") != marker || auth.Load() != beforeRecovery["authenticated"] || uploads.Load() != beforeRecovery["uploads"] || manifests.Load() != beforeRecovery["manifestReads"] {
		t.Fatal("post-recovery inspection differs from the retained artifact/runtime")
	}
	fresh.command(t, "resolve")
	resolved := fresh.event(t, ctx, "resolved")
	fresh.wait(t)
	if !resolved.KeyReleased || !resolved.ReceiptRetained || resolved.SecondJobStatus != jobqueue.StatusCancelled || resolved.Attempts != 1 {
		t.Fatal("inspection resolution did not release/dispose exact second job")
	}
	for _, n := range resolved.Effects {
		if n != 0 {
			t.Fatal("inspection resolution replayed source/build/runtime effects")
		}
	}
	final, e := jobs.Get(ctx, receipt.JobID)
	finalRun, re := st.Runs.Get(ctx, run.ID)
	finalHistory, he := st.AppDeploys.Get(ctx, history[0].ID)
	if e != nil || re != nil || he != nil || final.Status != jobqueue.StatusSucceeded || finalRun.Status != model.RunStatusSuccess || finalHistory.Status != model.RunStatusSuccess || final.Attempts != 1 {
		t.Fatal("recorded inspection outcome diverged")
	}
	appLossPreserved(t, run, history[0], finalRun, finalHistory)
	finalDigest, e := jobqueue.PayloadDigest(final.Payload)
	if e != nil || finalDigest != digest || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, workload) != containerID || nativeRuntimeCommand(ctx, t, "docker", "exec", workload, "/server", "probe") != marker || auth.Load() != beforeRecovery["authenticated"] || uploads.Load() != beforeRecovery["uploads"] || manifests.Load() != beforeRecovery["manifestReads"] {
		t.Fatal("recovery altered accepted input or repeated external effects")
	}
	graph, e := st.Pipelines.Get(ctx, receipt.PipelineID)
	if e != nil {
		t.Fatal("recorded App graph missing")
	}
	var logs strings.Builder
	for _, stage := range run.StageRuns {
		entries, e := logstore.NewPostgres(db, logstore.Limits{}).Read(ctx, run.ID, stage.StageID, 0)
		if e != nil || len(entries) == 0 {
			t.Fatal("actual stored stage logs absent")
		}
		for _, entry := range entries {
			logs.WriteString(entry.Line + "\n")
		}
	}
	if !strings.Contains(logs.String(), "built "+marker) {
		t.Fatal("actual BuildKit stdout missing")
	}
	var storedRun, storedGraph string
	if db.QueryRowContext(ctx, `SELECT row_to_json(r)::text FROM pipeline_runs r WHERE id=$1`, run.ID).Scan(&storedRun) != nil || db.QueryRowContext(ctx, `SELECT row_to_json(p)::text FROM pipelines p WHERE id=$1`, graph.ID).Scan(&storedGraph) != nil {
		t.Fatal("durable observation unavailable")
	}
	public, _ := json.Marshal([]any{receipt, run, attentionRun, finalRun, history, attentionHistory, finalHistory, graph})
	safe := append([]byte(string(final.Payload)+storedRun+storedGraph+logs.String()), public...)
	safe = append(safe, first.logs.Bytes()...)
	safe = append(safe, fresh.logs.Bytes()...)
	canaries := []string{password, appCanary, sourceCanary, username + ":" + password, base64.StdEncoding.EncodeToString([]byte(username + ":" + password)), base64.StdEncoding.EncodeToString(key), hex.EncodeToString(key), base}
	canaries = append(canaries, barrier.Checkouts...)
	canaries = append(canaries, barrier.Artifacts...)
	for _, value := range canaries {
		if value != "" && bytes.Contains(safe, []byte(value)) {
			t.Fatal("private canary/path entered retained safe process-loss data")
		}
	}
	data, _ := json.MarshalIndent(map[string]any{"scope": "owned-manual-App-after-terminal-before-Complete-SIGKILL-v1", "commit": commit, "receipt": receipt, "jobId": receipt.JobID, "runId": run.ID, "appId": app.ID, "firstWorkerPid": barrier.PID, "recoveryWorkerPid": recovered.PID, "parentPid": os.Getpid(), "signal": "SIGKILL", "actualClaimAttempts": 1, "firstWorkerEffects": barrier.Effects, "recoveryEffects": resolved.Effects, "pushCounters": pushSnapshot, "beforeRecoveryCounters": beforeRecovery, "afterRecoveryCounters": map[string]int32{"authenticated": auth.Load(), "uploads": uploads.Load(), "manifestReads": manifests.Load()}, "digest": pushed.Digest, "imageRef": image, "containerId": containerID, "businessBeforeLoss": "success", "businessAfterRecovery": "needs_attention", "businessAfterInspection": "success", "jobAfterRecovery": "needs_attention", "firstHistoryIdentity": history[0].ID, "artifactSourceReceiptPreserved": true, "manualAppSourceReceiptAbsent": true, "staleClaimFences": recovered.Fences, "resolutionFailureRolledBack": recovered.ResolutionRollback, "sameKeyBlockedUntilInspection": true, "runtimeInspectedBeforeResolution": true, "secondJobId": second.JobID, "secondJobDisposedWithoutDispatch": true, "temporaryPathsCleanedBeforeKill": true, "safeCanaryScanPassed": true, "remainingGates": []string{"other interruptions", "in-flight revocation combined with this interruption", "private FROM combined with this interruption", "hosted and owner acceptance", "upgrade and full release"}}, "", "  ")
	if os.WriteFile(os.Getenv("COOKER_APP_RUNTIME_EVIDENCE"), append(data, '\n'), 0600) != nil {
		t.Fatal("safe process-loss evidence write failed")
	}
}

func appLossPreserved(t *testing.T, before *model.PipelineRun, old *model.AppDeploy, after *model.PipelineRun, current *model.AppDeploy) {
	t.Helper()
	if before.ID != after.ID || before.PipelineID != after.PipelineID || before.StartedByUserSub != after.StartedByUserSub || before.StartedByEmail != after.StartedByEmail || before.Source != nil || after.Source != nil || !reflect.DeepEqual(before.StageRuns, after.StageRuns) || old.ID != current.ID || old.AppID != current.AppID || old.RunID != current.RunID || old.PipelineID != current.PipelineID || old.ImageRef != current.ImageRef || old.Digest != current.Digest || old.Kind != current.Kind || !old.CreatedAt.Equal(current.CreatedAt) {
		t.Fatal("recovery replaced recorded artifact/source/actor evidence")
	}
}

type appLossInput struct {
	Phase, DSN, Nonce, Schema, Repository, Base, Commit, Marker, BuildKit, Network, Workload, ImagePrefix, AppCanary, ConnectionID, RepositoryID, IntentID, JobID, RunID, PipelineID, PayloadDigest, OriginalOwner, SecondJobID string
	Key                                                                                                                                                                                                                         []byte
	ParentPID, ConnectionVersion                                                                                                                                                                                                int
}
type appLossEvent struct {
	Phase, JobID, Owner, Digest, Image                       string
	PID, ParentPID, Attempts                                 int
	Effects                                                  map[string]int32
	Checkouts, Artifacts                                     []string // private inherited IPC only; never copied into evidence.
	Fences, ResolutionRollback, KeyReleased, ReceiptRetained bool
	SecondJobStatus                                          jobqueue.Status
}
type appLossChild struct {
	cmd      *exec.Cmd
	input    *os.File
	events   chan appLossEvent
	done     chan error
	logs     bytes.Buffer
	finished bool
}

func appLossStart(t *testing.T, input appLossInput) *appLossChild {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal("child executable unavailable")
	}
	inR, inW, e := os.Pipe()
	if e != nil {
		t.Fatal("private input pipe unavailable")
	}
	outR, outW, e := os.Pipe()
	if e != nil {
		t.Fatal("private event pipe unavailable")
	}
	controlR, controlW, e := os.Pipe()
	if e != nil {
		t.Fatal("private command pipe unavailable")
	}
	home, e := os.MkdirTemp(input.Base, "child-home-")
	if e != nil {
		t.Fatal("private child home unavailable")
	}
	docker, e := os.MkdirTemp(input.Base, "child-docker-")
	if e != nil {
		t.Fatal("private child Docker config unavailable")
	}
	c := &appLossChild{input: controlW, events: make(chan appLossEvent, 8), done: make(chan error, 1)}
	c.cmd = exec.Command(exe, "-test.run=^TestAppSourceProcessLossChild$", "-test.v", "-test.timeout=180s")
	c.cmd.ExtraFiles = []*os.File{inR, outW, controlR}
	c.cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home, "DOCKER_CONFIG=" + docker, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "COOKER_OUTPUTS_ENABLED=true", "COOKER_APP_LOSS_CHILD=owned-private-ipc-v1", "TMPDIR=" + input.Base}
	c.cmd.Stdout = &c.logs
	c.cmd.Stderr = &c.logs
	if c.cmd.Start() != nil {
		t.Fatal("owned child start failed")
	}
	inR.Close()
	outW.Close()
	controlR.Close()
	go func() { c.done <- c.cmd.Wait() }()
	go func() {
		defer outR.Close()
		decoder := json.NewDecoder(outR)
		for {
			var v appLossEvent
			if decoder.Decode(&v) != nil {
				return
			}
			c.events <- v
		}
	}()
	if json.NewEncoder(inW).Encode(input) != nil {
		inW.Close()
		c.cleanup()
		t.Fatal("private child input unavailable")
	}
	inW.Close()
	return c
}
func (c *appLossChild) event(t *testing.T, ctx context.Context, phase string) appLossEvent {
	t.Helper()
	select {
	case v := <-c.events:
		if v.Phase != phase {
			t.Fatal("unexpected owned child phase")
		}
		return v
	case e := <-c.done:
		c.finished = true
		_ = e
		t.Fatal("owned child exited before barrier; unfiltered output withheld")
	case <-ctx.Done():
		t.Fatal("owned child barrier deadline")
	}
	return appLossEvent{}
}
func (c *appLossChild) command(t *testing.T, command string) {
	t.Helper()
	if json.NewEncoder(c.input).Encode(command) != nil {
		t.Fatal("private child control failed")
	}
}
func (c *appLossChild) kill(t *testing.T) {
	t.Helper()
	if c.cmd.Process.Signal(syscall.SIGKILL) != nil {
		t.Fatal("owned child SIGKILL failed")
	}
	select {
	case e := <-c.done:
		c.finished = true
		var exit *exec.ExitError
		if !errors.As(e, &exit) {
			t.Fatal("owned child was not killed")
		}
		status, ok := exit.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatal("owned child did not exit by SIGKILL")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned child kill deadline")
	}
}
func (c *appLossChild) wait(t *testing.T) {
	t.Helper()
	select {
	case e := <-c.done:
		c.finished = true
		if e != nil {
			t.Fatal("fresh recovery child failed; unfiltered output withheld")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fresh recovery child exit deadline")
	}
}
func (c *appLossChild) cleanup() {
	c.input.Close()
	if !c.finished {
		_ = c.cmd.Process.Kill()
		<-c.done
		c.finished = true
	}
}

type appLossEffects struct{ handler, sourceResolve, checkout, build, push, deploy, secretGet, secretList, imagePreparations, stageProgress, saveRun, savePipeline atomic.Int32 }

func (c *appLossEffects) snapshot() map[string]int32 {
	return map[string]int32{"handler": c.handler.Load(), "sourceResolve": c.sourceResolve.Load(), "checkout": c.checkout.Load(), "build": c.build.Load(), "push": c.push.Load(), "deploy": c.deploy.Load(), "secretGet": c.secretGet.Load(), "secretList": c.secretList.Load(), "imagePreparations": c.imagePreparations.Load(), "stageProgress": c.stageProgress.Load(), "saveRun": c.saveRun.Load(), "savePipeline": c.savePipeline.Load()}
}

type appLossManager struct {
	secrets.Manager
	recovery bool
	effects  *appLossEffects
}

func (*appLossManager) EnvironmentGenerationBound() bool { return true }
func (m *appLossManager) Get(ctx context.Context, id, key string) ([]byte, error) {
	m.effects.secretGet.Add(1)
	if m.recovery {
		return nil, errors.New("recovery cannot resolve runtime values")
	}
	return m.Manager.Get(ctx, id, key)
}
func (m *appLossManager) List(ctx context.Context, id string) ([]string, error) {
	m.effects.secretList.Add(1)
	if m.recovery {
		return nil, errors.New("recovery cannot discover secret keys")
	}
	return m.Manager.List(ctx, id)
}

type appLossBuild struct {
	real     *appRuntimeBuild
	recovery bool
	effects  *appLossEffects
}

func (b *appLossBuild) Build(ctx context.Context, r builder.Request) (builder.Result, error) {
	b.effects.build.Add(1)
	if b.recovery {
		return builder.Result{}, errors.New("recovery cannot build")
	}
	return b.real.Build(ctx, r)
}

type appLossPush struct {
	recovery bool
	effects  *appLossEffects
	event    func(appLossEvent) error
	command  *json.Decoder
}

func (p *appLossPush) Push(ctx context.Context, r pusher.Request) (pusher.Result, error) {
	p.effects.push.Add(1)
	if p.recovery {
		return pusher.Result{}, errors.New("recovery cannot push")
	}
	v, e := pusher.NewCrane().Push(ctx, r)
	if e == nil && p.event != nil {
		if p.event(appLossEvent{Phase: "pushed", Digest: v.Digest, Effects: p.effects.snapshot()}) != nil {
			return v, errors.New("private push event failed")
		}
		var command string
		if p.command.Decode(&command) != nil || command != "deploy" {
			return v, errors.New("private deployment control failed")
		}
	}
	return v, e
}

type appLossDeploy struct {
	input    appLossInput
	recovery bool
	effects  *appLossEffects
	image    string
}

func (d *appLossDeploy) Deploy(ctx context.Context, r deployer.Request) (deployer.Result, error) {
	d.effects.deploy.Add(1)
	if d.recovery {
		return deployer.Result{}, errors.New("recovery cannot deploy")
	}
	if r.Kind != deployer.KindDockerRun || r.Name != d.input.Workload || r.RegistryAuth == nil || !strings.HasPrefix(r.Image, d.input.ImagePrefix+"@sha256:") || len(r.Env) != 1 || r.Env["APP_LOG_CANARY"] != d.input.AppCanary || len(r.Ports) != 0 || r.Labels[RuntimeRunLabel] != d.input.RunID || nativeRuntimeExists(ctx, "container", r.Name) || nativeRuntimeExists(ctx, "image", r.Image) {
		return deployer.Result{}, errors.New("unowned real App deployment request")
	}
	d.image = r.Image
	if r.Labels == nil {
		r.Labels = map[string]string{}
	}
	r.Labels["cooker.fixture"], r.Labels["cooker.agent"], r.Network = d.input.Nonce, "roadmap_audit", d.input.Network
	r.Resources = &deployer.ResourceLimits{Memory: "128m", CPUs: "0.5"}
	return deployer.NewDockerRun().Deploy(ctx, r)
}

type appLossParts struct {
	effects *appLossEffects
	source  *appRuntimeGit
	build   *appRuntimeBuild
	deploy  *appLossDeploy
}

func appLossQueue(t *testing.T, ctx context.Context, in appLossInput, st *store.Store, db *sql.DB, codec *cookercrypto.Codec, recovery bool, event func(appLossEvent) error, command *json.Decoder) (*AppDeployQueue, *jobqueue.Postgres, *appLossParts) {
	t.Helper()
	effects := &appLossEffects{}
	manager := &appLossManager{Manager: database.New(storepg.NewEnvironmentStore(db), codec), effects: effects, recovery: recovery}
	authority := NewRegistryAuthorityService(st, manager)
	authority.Registries, authority.Environments = storepg.NewRegistryConfigStore(db), storepg.NewEnvironmentStore(db)
	jobs := jobqueue.NewPostgres(db)
	jobs.OnTransition = storepg.SyncJobRun
	jobs.OnEnqueue = func(c context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		if e := storepg.SyncRegistryAuthorityAdmission(c, tx, j); e != nil {
			return e
		}
		return storepg.SyncTriggerAdmission(c, tx, j)
	}
	if !storepg.RegistryAdmissionStoresMatch(jobs, authority.Registries, authority.Environments) {
		t.Fatal("owned registry admission store mismatch")
	}
	source := &appRuntimeGit{root: in.Repository, base: in.Base, connections: &connection.Postgres{DB: db}, connectionID: in.ConnectionID, version: in.ConnectionVersion, repositoryID: in.RepositoryID, acceptedCommit: in.Commit}
	build := &appRuntimeBuild{delegate: builder.NewBuildKit(in.BuildKit), marker: in.Marker, commit: in.Commit}
	deploy := &appLossDeploy{input: in, recovery: recovery, effects: effects}
	executor := NewExecutor(WithBuilder(&appLossBuild{real: build, recovery: recovery, effects: effects}), WithPusher(&appLossPush{recovery: recovery, effects: effects, event: event, command: command}), WithDockerDeployer(deploy), WithRegistryAuthority(authority), WithLogStore(logstore.NewPostgres(db, logstore.Limits{})), WithRunUpdater(func(c context.Context, r *model.PipelineRun) error {
		effects.stageProgress.Add(1)
		if recovery {
			return errors.New("recovery cannot publish stage progress")
		}
		return st.Runs.UpdateProgress(c, r.ID, r.StageRuns)
	}))
	d := NewAppDeployer(executor, "")
	d.Apps, d.Deploys = st.Apps, st.AppDeploys
	d.EnvResolver = &AppEnvResolver{Environments: storepg.NewEnvironmentStore(db), Secrets: manager}
	d.SaveRun = func(c context.Context, r *model.PipelineRun) error {
		effects.saveRun.Add(1)
		if recovery {
			return errors.New("recovery cannot execute terminal writes")
		}
		return st.Runs.Update(c, r)
	}
	d.SavePipeline = func(c context.Context, p *model.Pipeline) error {
		effects.savePipeline.Add(1)
		if recovery {
			return errors.New("recovery cannot execute graph writes")
		}
		old, e := st.Pipelines.Get(c, p.ID)
		if errors.Is(e, store.ErrNotFound) {
			return st.Pipelines.Create(c, p)
		}
		if e != nil {
			return e
		}
		p.Version = old.Version
		return st.Pipelines.Update(c, p)
	}
	d.CheckExecution = func(a *model.App, _ bool) error {
		if a.ID == "" || a.BuildPlan == nil || a.BuildPlan.Kind != model.BuildPlanDockerfile || a.DeployTarget.Kind != model.DeployTargetDockerHost || a.DeployTarget.Prefix != in.Workload {
			return errors.New("unowned App execution capability")
		}
		return nil
	}
	d.SourceCheckout = func(c context.Context, a *model.App, sha string, w io.Writer) (string, error) {
		effects.checkout.Add(1)
		if recovery {
			return "", errors.New("recovery cannot checkout")
		}
		return source.Checkout(c, a, sha, w)
	}
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, RegistryAuthority: authority, RequireRegistryPins: true, RequireSecretPins: true, RequireImagePins: true, AdmissionGuard: storepg.NewTriggerStore(db), Deadline: 2 * time.Minute}
	q.ResolveSource = func(c context.Context, a *model.App, ref string) (string, error) {
		effects.sourceResolve.Add(1)
		if recovery {
			return "", errors.New("recovery cannot rediscover refs")
		}
		return source.Resolve(c, a, ref)
	}
	q.PrepareImages = func(c context.Context, p *AppDeployPlan) (*AppImageResolution, error) {
		effects.imagePreparations.Add(1)
		if recovery {
			return nil, errors.New("recovery cannot repin images")
		}
		return d.PrepareImages(c, p, RegistryImageResolver(nil))
	}
	return q, jobs, &appLossParts{effects: effects, source: source, build: build, deploy: deploy}
}

// The wrapper only controls a fixture barrier; it never substitutes Complete.
// Blocking disregards the record timeout deliberately, until the actual OS kill.
type appLossCompleteBarrier struct {
	*jobqueue.Postgres
	before func(context.Context, string, string) error
}

func (s *appLossCompleteBarrier) Complete(ctx context.Context, owner, id string) error {
	if e := s.before(ctx, owner, id); e != nil {
		return e
	}
	select {}
}

func TestAppSourceProcessLossChild(t *testing.T) {
	if os.Getenv("COOKER_APP_LOSS_CHILD") != "owned-private-ipc-v1" {
		t.Skip("owned private IPC child only")
	}
	for _, fd := range []int{3, 4, 5} {
		syscall.CloseOnExec(fd)
	}
	inFile, out, control := os.NewFile(3, "private-input"), os.NewFile(4, "private-events"), os.NewFile(5, "private-control")
	defer inFile.Close()
	defer out.Close()
	defer control.Close()
	var in appLossInput
	if json.NewDecoder(inFile).Decode(&in) != nil || len(in.Key) != 32 || in.ParentPID != os.Getppid() || in.Phase != "worker" && in.Phase != "recovery" {
		t.Fatal("invalid private owned child input")
	}
	u, e := url.Parse(in.DSN)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User == nil || u.User.Username() != "app_runtime_fixture" || u.Path != "/cooker_app_runtime_test" || u.Query().Get("search_path") != in.Schema || in.Schema != "app_loss_"+strings.ReplaceAll(in.Nonce, "-", "") {
		t.Fatal("unowned child database")
	}
	if _, has := u.User.Password(); has {
		t.Fatal("owned child database cannot inherit a password")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	st, e := storepg.NewStore(ctx, in.DSN)
	if e != nil {
		t.Fatal("fresh owned store unavailable")
	}
	defer st.Close()
	db, e := sql.Open("postgres", in.DSN)
	if e != nil {
		t.Fatal("fresh queue pool unavailable")
	}
	defer db.Close()
	codec, e := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(in.Key))
	if e != nil {
		t.Fatal("fresh owned codec unavailable")
	}
	encoder := json.NewEncoder(out)
	send := func(event appLossEvent) error {
		event.PID, event.ParentPID = os.Getpid(), os.Getppid()
		return encoder.Encode(event)
	}
	command := json.NewDecoder(control)
	recovery := in.Phase == "recovery"
	q, jobs, parts := appLossQueue(t, ctx, in, st, db, codec, recovery, send, command)
	accepted, e := jobs.Get(ctx, in.JobID)
	if e != nil {
		t.Fatal("fresh accepted job unavailable")
	}
	digest, e := jobqueue.PayloadDigest(accepted.Payload)
	if e != nil || digest != in.PayloadDigest {
		t.Fatal("fresh process input digest differs")
	}
	var plan AppDeployPlan
	if json.Unmarshal(accepted.Payload, &plan) != nil || plan.App == nil || plan.Commit != in.Commit || plan.RunID != in.RunID || plan.Registry == nil || plan.Secrets == nil || plan.Images == nil {
		t.Fatal("fresh accepted App plan unavailable")
	}
	var queueStore jobqueue.Store = jobs
	if !recovery {
		queueStore = &appLossCompleteBarrier{Postgres: jobs, before: func(c context.Context, owner, id string) error {
			j, e := jobs.Get(c, id)
			r, re := st.Runs.Get(c, in.RunID)
			h, he := st.AppDeploys.ListByApp(c, plan.App.ID, 100)
			if e != nil || re != nil || he != nil || id != in.JobID || j.LockedBy != owner || j.Status != jobqueue.StatusRunning || j.Attempts != 1 || r.Status != model.RunStatusSuccess || len(h) != 1 || h[0].Status != model.RunStatusSuccess {
				return errors.New("real terminal result barrier unavailable")
			}
			return send(appLossEvent{Phase: "before-complete", JobID: id, Owner: owner, Attempts: j.Attempts, Digest: h[0].Digest, Image: h[0].ImageRef, Effects: parts.effects.snapshot(), Checkouts: parts.source.checkouts, Artifacts: parts.build.artifacts})
		}}
	}
	reg := jobqueue.NewRegistry()
	reg.Register(JobKindAppDeploy, func(c context.Context, j *jobqueue.Job) error { parts.effects.handler.Add(1); return q.Handle(c, j) })
	pool, e := jobqueue.NewPool(jobqueue.PoolOptions{Store: queueStore, Registry: reg, Size: 1, WorkerIDPrefix: "owned-App-loss-" + in.Phase, PollEvery: 25 * time.Millisecond, LeaseTimeout: 30 * time.Second, HeartbeatEvery: 500 * time.Millisecond, OnHeartbeat: HeartbeatJobRun(st), OnRecovery: RecoverJobRun(st), ClaimContext: func(c context.Context, j *jobqueue.Job) context.Context {
		return store.WithRunClaim(c, j.ID, j.LockedBy, nil)
	}})
	if e != nil {
		t.Fatal("fresh real pool unavailable")
	}
	poolCtx, stopPool := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- pool.Run(poolCtx) }()
	if !recovery {
		<-ctx.Done()
		t.Fatal("actual OS kill was required before Complete")
		return
	}
	stopped := false
	defer func() {
		if !stopped {
			stopPool()
			<-done
		}
	}()
	for {
		j, e := jobs.Get(ctx, in.JobID)
		if e != nil {
			t.Fatal("fresh recovery job unavailable")
		}
		if j.Status == jobqueue.StatusNeedsAttention {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual lease expiry deadline")
		case <-time.After(50 * time.Millisecond):
		}
	}
	second, e := jobs.Get(ctx, in.SecondJobID)
	if e != nil || second.Status != jobqueue.StatusPending || second.Attempts != 0 {
		t.Fatal("fresh Pool did not reserve uncertain key")
	}
	old, e := st.Runs.Get(ctx, in.RunID)
	h, he := st.AppDeploys.ListByApp(ctx, plan.App.ID, 100)
	if e != nil || he != nil || len(h) != 1 || old.Status != model.RunStatusNeedsAttention || h[0].Status != model.RunStatusNeedsAttention {
		t.Fatal("fresh SQL lifecycle observation missing")
	}
	stale := store.WithRunClaim(ctx, in.JobID, in.OriginalOwner, nil)
	if e := jobs.Complete(ctx, in.OriginalOwner, in.JobID); !errors.Is(e, jobqueue.ErrNotLocked) {
		t.Fatal("lost Complete claim regained authority")
	}
	if e := st.Runs.UpdateProgress(stale, in.RunID, old.StageRuns); !errors.Is(e, store.ErrConflict) {
		t.Fatal("stale progress write regained authority")
	}
	forged := old.Clone()
	forged.Status = model.RunStatusSuccess
	if e := st.Runs.Update(stale, forged); !errors.Is(e, store.ErrConflict) {
		t.Fatal("stale terminal write regained authority")
	}
	before, e := jobs.Get(ctx, in.JobID)
	if e != nil {
		t.Fatal("recovery snapshot missing")
	}
	jobs.OnTransition = func(c context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		if e := storepg.SyncJobRun(c, tx, j); e != nil {
			return e
		}
		return errors.New("owned inspection-resolution persistence fault")
	}
	if jobs.Resolve(ctx, in.JobID, jobqueue.StatusSucceeded, "owned exact runtime was inspected") == nil {
		t.Fatal("injected resolution failure committed")
	}
	rollback, e := jobs.Get(ctx, in.JobID)
	r, re := st.Runs.Get(ctx, in.RunID)
	hh, he := st.AppDeploys.ListByApp(ctx, plan.App.ID, 100)
	if e != nil || re != nil || he != nil || !reflect.DeepEqual(before, rollback) || !reflect.DeepEqual(old, r) || !reflect.DeepEqual(h, hh) {
		t.Fatal("failed resolution did not roll back job/run/history coherently")
	}
	jobs.OnTransition = storepg.SyncJobRun
	for _, n := range parts.effects.snapshot() {
		if n != 0 {
			t.Fatal("fresh recovery attempted source/build/deploy effects")
		}
	}
	if send(appLossEvent{Phase: "recovered", Attempts: before.Attempts, Fences: true, ResolutionRollback: true, Effects: parts.effects.snapshot()}) != nil {
		t.Fatal("private recovery observation failed")
	}
	var action string
	if command.Decode(&action) != nil || action != "resolve" {
		t.Fatal("private inspection resolution control failed")
	}
	// Stop polling before release, so the second claim proves the key without dispatch.
	stopPool()
	if <-done != nil {
		t.Fatal("fresh recovery Pool stop failed")
	}
	stopped = true
	if jobs.Resolve(ctx, in.JobID, jobqueue.StatusSucceeded, "owned fixture inspected the exact run-labelled digest and endpoint; acknowledgement was lost") != nil {
		t.Fatal("owned inspection resolution failed")
	}
	receipt, e := q.EnqueueIntent(ctx, plan.App, plan.Commit, nil, in.IntentID)
	if e != nil || receipt.RunID != in.RunID || receipt.JobID != in.JobID || receipt.PlanDigest != in.PayloadDigest {
		t.Fatal("fresh receipt-first recovery did not retain identity")
	}
	next, e := jobs.Dequeue(ctx, "owned-second-key-proof", []string{JobKindAppDeploy})
	if e != nil || next.ID != in.SecondJobID || next.Attempts != 1 {
		t.Fatal("inspection resolution did not release same key")
	}
	status, e := jobs.Cancel(ctx, next.ID)
	if e != nil || status != jobqueue.StatusNeedsAttention {
		t.Fatal("claimed second job cancellation did not retain ambiguity")
	}
	// No handler was dispatched: record the inspected no-effects outcome explicitly.
	if jobs.Resolve(ctx, next.ID, jobqueue.StatusCancelled, "owned fixture claimed only to test key release; no handler dispatched") != nil {
		t.Fatal("owned second-job disposal failed")
	}
	second, e = jobs.Get(ctx, next.ID)
	if e != nil || second.Status != jobqueue.StatusCancelled {
		t.Fatal("owned second-job disposal missing")
	}
	if send(appLossEvent{Phase: "resolved", Attempts: before.Attempts, KeyReleased: true, ReceiptRetained: true, SecondJobStatus: second.Status, Effects: parts.effects.snapshot()}) != nil {
		t.Fatal("private final observation failed")
	}
}
