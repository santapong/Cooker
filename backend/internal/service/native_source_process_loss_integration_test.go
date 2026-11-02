//go:build integration

package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"github.com/santapong/cooker/internal/store/memory"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// A genuine first OS worker is killed after actual Docker readiness while its
// deployment result is withheld. A different OS process runs real PG lease
// recovery and explicit inspection resolution, never a replay or Handle-only.
func TestNativeSourceRegistryProcessLossAfterReadiness(t *testing.T) {
	if os.Getenv("COOKER_NATIVE_PROCESS_LOSS_ACCEPTANCE") != "owned-local-v1" {
		t.Skip("explicit owned genuine process-loss harness required")
	}
	nonce := os.Getenv("COOKER_NATIVE_RUNTIME_NONCE")
	if u, err := uuid.Parse(nonce); err != nil || u.String() != nonce || u == uuid.Nil {
		t.Fatal("invalid fixture ownership nonce")
	}
	prefix := "cooker-native-loss-" + nonce
	network := prefix + "-net"
	if os.Getenv("COOKER_NATIVE_RUNTIME_NETWORK") != network {
		t.Fatal("unowned fixture network")
	}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Minute)
	defer stop()
	if got := nativeRuntimeCommand(ctx, t, "docker", "network", "inspect", "--format", `{{index .Labels "cooker.fixture"}}`, network); got != nonce {
		t.Fatal("fixture network ownership is absent")
	}
	if got := nativeRuntimeCommand(ctx, t, "docker", "network", "inspect", "--format", `{{.Internal}}`, network); got != "true" {
		t.Fatal("fixture network has external routing")
	}
	buildkitURL, err := url.Parse(os.Getenv("COOKER_NATIVE_RUNTIME_BUILDKIT"))
	if err != nil || buildkitURL.Scheme != "tcp" || buildkitURL.Hostname() != "127.0.0.1" || buildkitURL.Port() == "" || buildkitURL.User != nil || buildkitURL.Path != "" || buildkitURL.RawQuery != "" {
		t.Fatal("unowned BuildKit control endpoint")
	}
	dsn, err := nativeRuntimeOwnedDatabaseURL(os.Getenv("COOKER_NATIVE_RUNTIME_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "native_loss_" + strings.ReplaceAll(nonce, "-", "")
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("owned schema cleanup", err)
		}
	}()
	q := dsn.Query()
	q.Set("search_path", schema)
	dsn.RawQuery = q.Encode()
	st, err := storepg.NewStore(ctx, dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)

	// Synthetic credentials never enter source, job payload, environment or logs.
	password := "native-runtime-private-" + uuid.NewString()
	username := "native-runtime-fixture"
	var authenticated, uploads, manifestReads atomic.Int32
	reg := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != username || p != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="owned-native-fixture"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authenticated.Add(1)
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/manifests/") {
			uploads.Add(1)
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/manifests/") {
			manifestReads.Add(1)
		}
		reg.ServeHTTP(w, r)
	}))
	defer registryServer.Close()
	registryHost := strings.TrimPrefix(registryServer.URL, "http://")
	image := registryHost + "/owned/native-" + nonce + ":accepted"
	workload := prefix + "-web"
	if nativeRuntimeExists(ctx, "container", workload) || nativeRuntimeExists(ctx, "image", image) {
		t.Fatal("fixture identity already exists")
	}
	var deployedImage string
	defer func() {
		ref := ""
		if deployedImage != "" {
			ref = deployedImage
		}
		nativeRuntimeCleanup(t, nonce, workload, image, ref)
	}()

	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	if err := os.Mkdir(repository, 0700); err != nil {
		t.Fatal(err)
	}
	server, err := os.ReadFile(os.Getenv("COOKER_NATIVE_RUNTIME_SERVER_BINARY"))
	if err != nil || len(server) == 0 {
		t.Fatal("owned static server input unavailable")
	}
	marker := "accepted-native-commit-" + nonce
	for path, contents := range map[string][]byte{
		"server":      server,
		"message.txt": []byte(marker),
		"Dockerfile":  []byte("FROM scratch\nCOPY server /server\nCOPY message.txt /message.txt\nRUN [\"/server\",\"build-proof\"]\nHEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=15 CMD [\"/server\",\"probe\"]\nENTRYPOINT [\"/server\"]\n"),
	} {
		if err := os.WriteFile(filepath.Join(repository, path), contents, 0700); err != nil {
			t.Fatal(err)
		}
	}
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "init", "--initial-branch=main")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "add", "Dockerfile", "server", "message.txt")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "-c", "user.name=Owned fixture", "-c", "user.email=fixture@invalid.local", "commit", "-m", "owned runtime input")
	commit := nativeRuntimeCommand(ctx, t, "git", "-C", repository, "rev-parse", "HEAD")
	codecKey := make([]byte, 32)
	if _, err = rand.Read(codecKey); err != nil {
		t.Fatal(err)
	}
	codec, err := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(codecKey))
	if err != nil {
		t.Fatal(err)
	}
	connections := &connection.Service{Store: &connection.Postgres{DB: db}, Codec: codec}
	c := &connection.Connection{Name: "owned runtime source", Provider: "github", Enabled: true, Repositories: []string{"owned/native-runtime"}}
	if err := connections.Create(ctx, c, "synthetic-fixture-connection-value"); err != nil {
		t.Fatal(err)
	}
	source := &nativeRuntimeGit{root: repository, base: base, repositoryID: "owned-local-" + nonce}
	preparer := &PipelineSourcePreparer{Store: st, Connections: connections, Source: source}
	p := &model.Pipeline{ID: uuid.NewString(), Name: "Owned native runtime", Source: &model.PipelineSourceConfig{ConnectionID: c.ID, Repository: "owned/native-runtime", FullRef: "refs/heads/main"}, Stages: []model.Stage{
		{ID: "build", Name: "Build exact source", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Dockerfile: "Dockerfile", Tags: []string{image}}},
		{ID: "push", Name: "Publish OCI artifact", Type: model.StageTypePush, Config: model.StageConfig{Image: "${stages.build.tag}", Repository: image}},
		{ID: "deploy", Name: "Run immutable image", Type: model.StageTypeDeploy, Config: model.StageConfig{Image: "${stages.push.image}", RuntimeName: workload, DeployRuntime: "docker", Resources: &model.ResourceLimits{Memory: "128m", CPUs: "0.5"}}},
	}, Edges: []model.Edge{{ID: "build-push", Source: "build", Target: "push"}, {ID: "push-deploy", Source: "push", Target: "deploy"}}}
	p.SetDefaults()
	incarnation := uuid.NewString()
	envID := "registry_" + strings.ReplaceAll(incarnation, "-", "")
	env := &model.Environment{ID: envID, Name: "owned registry private", PlainVars: map[string]string{}, CreatedAt: time.Now().UTC()}
	if err := st.Environments.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	manager := database.New(storepg.NewEnvironmentStore(db), codec)
	if err := manager.Put(ctx, envID, "password", []byte(password)); err != nil {
		t.Fatal(err)
	}
	config := &model.RegistryConfig{ID: uuid.NewString(), Name: "Owned runtime registry", URL: registryServer.URL, Username: username, PasswordRef: "registry:v2:" + incarnation, AllowedPipelineIDs: []string{p.ID}}
	if err := st.Registries.Create(ctx, config); err != nil {
		t.Fatal(err)
	}
	for i := range p.Stages {
		p.Stages[i].Config.RegistryConnectionIDs = []string{config.ID}
	}
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	p, err = st.Pipelines.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	authority := NewRegistryAuthorityService(st, manager)
	authority.Registries, authority.Environments = storepg.NewRegistryConfigStore(db), storepg.NewEnvironmentStore(db)
	jobs := jobqueue.NewPostgres(db)
	jobs.OnEnqueue = func(ctx context.Context, tx *sql.Tx, job *jobqueue.Job) error {
		if err := storepg.SyncRegistryAuthorityAdmission(ctx, tx, job); err != nil {
			return err
		}
		return storepg.SyncTriggerAdmission(ctx, tx, job)
	}
	jobs.OnTransition = storepg.SyncJobRun
	if !storepg.RegistryAdmissionStoresMatch(jobs, authority.Registries, authority.Environments) {
		t.Fatal("authority pool mismatch")
	}
	a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, Jobs: jobs, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }, RequireRegistryPreflight: true, RegistryPreflight: func(ctx context.Context, p *model.Pipeline) error {
		return CheckPipelineRegistryPreflight(ctx, authority, p)
	}}
	rule := &model.TriggerRule{ID: uuid.NewString(), Name: "Owned main", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "automatic"}
	if _, err := a.Preview(ctx, rule, p.Source.FullRef); err != nil {
		t.Fatal("native preview", err)
	}
	now := time.Now().UTC()
	rule.ReviewedAt, rule.ReviewedBySubject = &now, "owned-fixture-admin"
	if err := a.Triggers.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("owned-delivery-" + nonce))
	delivery, _, err := a.Triggers.AcceptVerified(ctx, model.VerifiedGitEvent{Source: rule.Source, DeliveryID: "runtime-" + nonce, Event: "push", FullRef: p.Source.FullRef, CommitSHA: commit, PayloadHash: hex.EncodeToString(hash[:])})
	if err != nil || len(delivery.Decisions) != 1 {
		t.Fatal("native delivery", err)
	}
	intent, err := a.Triggers.Store.GetIntent(ctx, delivery.Decisions[0].IntentID)
	if err != nil {
		t.Fatal(err)
	}
	producer := NewJobQueueEnqueuer(jobs, st.Pipelines)
	producer.AdmissionGuard = storepg.NewTriggerStore(db)
	producer.RequireRegistryPins = true
	var pins atomic.Int32
	producer.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		pins.Add(1)
		return PreparePipelineRegistryAuthority(ctx, authority, p)
	}
	admissionExecutor := NewExecutor(WithRegistryAuthority(authority))
	producer.PrepareSecrets = admissionExecutor.PreparePipelineSecrets
	a.Enqueue = producer.EnqueueSourceRun
	if err := a.Dispatch(ctx, intent); err != nil {
		t.Fatal("first admission", err)
	}
	accepted, err := a.Triggers.Store.GetIntent(ctx, intent.ID)
	if err != nil || accepted.Receipt == nil {
		t.Fatal("first receipt absent")
	}
	first, err := jobs.Get(ctx, accepted.Receipt.JobID)
	if err != nil || first.Status != jobqueue.StatusPending || first.Attempts != 0 {
		t.Fatal("first admission not pristine")
	}
	var payload runJobPayload
	if json.Unmarshal(first.Payload, &payload) != nil || payload.Source == nil || payload.Registry == nil || payload.Run == nil || payload.Run.Source == nil || payload.Source.CommitSHA != commit || payload.Run.Source.CheckedOutAt != nil {
		t.Fatal("accepted source/registry pins absent")
	}
	digest, err := jobqueue.PayloadDigest(first.Payload)
	if err != nil {
		t.Fatal("accepted digest unavailable")
	}
	input := nativeLossChildInput{DSN: dsn.String(), Nonce: nonce, Schema: schema, Mode: "execute", JobID: first.ID, SecondJobID: "", RunID: payload.Run.ID, PipelineID: p.ID, RegistryID: config.ID, IntentID: intent.ID, Key: codecKey, ParentPID: os.Getpid(), Repository: repository, Base: base, RepositoryID: source.repositoryID, Buildkit: buildkitURL.String(), Marker: marker, Workload: workload, Network: network}
	child, barrier, childLogs := nativeLossStart(ctx, t, input, base)
	defer child.close()
	if barrier.FailureCategory != "" {
		_ = child.cmd.Wait()
		child.waited = true
		for _, line := range strings.Split(strings.ReplaceAll(childLogs.String(), `\n`, "\n"), "\n") {
			if strings.HasPrefix(line, "github.com/santapong/cooker/") {
				if at := strings.Index(line, "("); at > 0 {
					barrier.PanicFunctions = append(barrier.PanicFunctions, line[:at])
				}
			}
		}

		if path := os.Getenv("COOKER_NATIVE_RUNTIME_EVIDENCE"); path != "" {
			safe, _ := json.MarshalIndent(barrier, "", "  ")
			_ = os.WriteFile(path+".first-failure.json", append(safe, '\n'), 0600)
		}
		t.Fatal("first worker outcome before runtime barrier", barrier.FailureCategory, barrier.FailurePhases)
	}
	if barrier.PID != child.cmd.Process.Pid || barrier.PID == os.Getpid() || barrier.ParentPID != os.Getpid() || barrier.ClaimJobID != first.ID || barrier.ClaimAttempts != 1 || barrier.ClaimOwner == "" || barrier.Image == "" || !strings.HasSuffix(barrier.Image, "@"+barrier.Digest) || barrier.DockerHealth != "healthy" {
		t.Fatal("first child barrier invalid")
	}
	deployedImage = barrier.Image
	// Parent independently observes the real exact runtime before sending SIGKILL.
	containerID := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, workload)
	if nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{index .Config.Labels "cooker.fixture"}}`, containerID) != nonce || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{index .Config.Labels "io.cooker.run-id"}}`, containerID) != payload.Run.ID || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Config.Image}}`, containerID) != barrier.Image || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.State.Health.Status}}`, containerID) != "healthy" || nativeRuntimeCommand(ctx, t, "docker", "exec", containerID, "/server", "probe") != marker {
		t.Fatal("parent runtime inspection differs")
	}
	if !strings.Contains(nativeRuntimeCommand(ctx, t, "docker", "image", "inspect", "--format", `{{json .RepoDigests}}`, barrier.Image), "@"+barrier.Digest) {
		t.Fatal("actual Docker digest absent")
	}
	// A second genuine admission has the same pipeline key but a different exact
	// Git commit, receipt and run. It must not start while the first is uncertain.
	if os.WriteFile(filepath.Join(repository, "message.txt"), []byte("second-unexecuted-"+nonce), 0700) != nil {
		t.Fatal("second input unavailable")
	}
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "add", "message.txt")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "-c", "user.name=Owned fixture", "-c", "user.email=fixture@invalid.local", "commit", "-m", "second blocked input")
	secondCommit := nativeRuntimeCommand(ctx, t, "git", "-C", repository, "rev-parse", "HEAD")
	secondHash := sha256.Sum256([]byte("second-delivery-" + nonce))
	secondDelivery, _, err := a.Triggers.AcceptVerified(ctx, model.VerifiedGitEvent{Source: rule.Source, DeliveryID: "second-" + nonce, Event: "push", FullRef: p.Source.FullRef, CommitSHA: secondCommit, PayloadHash: hex.EncodeToString(secondHash[:])})
	if err != nil || len(secondDelivery.Decisions) != 1 {
		t.Fatal("second verified delivery unavailable", err)
	}
	secondIntent, err := a.Triggers.Store.GetIntent(ctx, secondDelivery.Decisions[0].IntentID)
	if err != nil || a.Dispatch(ctx, secondIntent) != nil {
		t.Fatal("second admission unavailable")
	}
	secondAccepted, err := a.Triggers.Store.GetIntent(ctx, secondIntent.ID)
	if err != nil || secondAccepted.Receipt == nil {
		t.Fatal("second receipt absent")
	}
	second, err := jobs.Get(ctx, secondAccepted.Receipt.JobID)
	if err != nil || second.Status != jobqueue.StatusPending || second.Attempts != 0 || second.ConcurrencyKey == "" || second.ConcurrencyKey != first.ConcurrencyKey || second.ID == first.ID || pins.Load() != 2 {
		t.Fatal("same-key second admission invalid")
	}
	input.SecondJobID = second.ID
	beforeKill, err := jobs.Get(ctx, first.ID)
	observedRun, errRun := st.Runs.Get(ctx, payload.Run.ID)
	if path := os.Getenv("COOKER_NATIVE_RUNTIME_EVIDENCE"); path != "" {
		safe := map[string]any{"scope": "native-before-kill-observation", "acceptedCommit": commit, "runId": payload.Run.ID, "receipt": accepted.Receipt, "runtime": barrier, "containerId": containerID, "claimStatus": beforeKill.Status, "attempts": beforeKill.Attempts, "verifiedCheckoutDurable": observedRun.Source != nil && observedRun.Source.CheckedOutAt != nil, "runStartedDurable": observedRun.StartedAt != nil}
		data, _ := json.MarshalIndent(safe, "", "  ")
		if os.WriteFile(path+".before-kill.json", append(data, '\n'), 0600) != nil {
			t.Fatal("barrier observation persistence failed")
		}
	}
	if err != nil || errRun != nil || beforeKill.Status != jobqueue.StatusRunning || beforeKill.Attempts != 1 || beforeKill.LockedBy != barrier.ClaimOwner || observedRun.Source == nil || observedRun.Source.CheckedOutAt == nil || observedRun.Source.CommitSHA != commit {
		t.Fatal("durable live claim/source proof absent")
	}
	if err := nativeLossStageEvidence(observedRun, barrier.Digest); err != nil {
		t.Fatal(err)
	}
	beforeLogs, beforeLogCount := nativeLossDurableLogs(ctx, t, db, payload.Run.ID)
	if beforeLogCount == 0 || !bytes.Contains(beforeLogs, []byte("built "+marker)) {
		t.Fatal("actual durable BuildKit stdout absent")
	}
	beforeLogHash := sha256.Sum256(beforeLogs)
	containerLogsBefore := nativeRuntimeCommand(ctx, t, "docker", "logs", containerID)
	if !strings.Contains(containerLogsBefore, "ready "+marker) {
		t.Fatal("actual container stdout absent")
	}
	beforeJSON, _ := json.Marshal(observedRun)
	registryBefore := map[string]int32{"authenticated": authenticated.Load(), "uploads": uploads.Load(), "manifestReads": manifestReads.Load()}
	if registryBefore["uploads"] != 1 || registryBefore["manifestReads"] < 1 || barrier.Builds != 1 || barrier.Pushes != 1 || barrier.Deploys != 1 {
		t.Fatal("genuine first effects not observed once")
	}
	if child.cmd.Process.Signal(syscall.SIGKILL) != nil {
		t.Fatal("owned worker SIGKILL failed")
	}
	waitErr := child.cmd.Wait()
	child.waited = true
	if waitErr == nil {
		t.Fatal("worker did not die by SIGKILL")
	}
	status, ok := child.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("loss was not actual SIGKILL")
	}
	// The recovery process cannot reach a new provider/runtime effect. Its real
	// native runner remains registered; instrumentation rejects/counts attempts.
	input.Mode = "recover"
	input.OldOwner = barrier.ClaimOwner
	input.Image = barrier.Image
	input.Digest = barrier.Digest
	input.BeforeRun = observedRun
	recovered, recoveryBarrier, recoveryLogs := nativeLossStart(ctx, t, input, base)
	defer recovered.close()
	if err := recovered.cmd.Wait(); err != nil {
		t.Fatal("fresh recovery process failed; raw child logs withheld")
	}
	recovered.waited = true
	if recoveryBarrier.PID != recovered.cmd.ProcessState.Pid() || recoveryBarrier.PID == barrier.PID || recoveryBarrier.PID == os.Getpid() || !recoveryBarrier.BlockedSecond || !recoveryBarrier.StaleCompleteFenced || !recoveryBarrier.StaleProgressFenced || !recoveryBarrier.StaleTerminalFenced || !recoveryBarrier.ResolutionRollback || !recoveryBarrier.ResolvedWithoutEffects || !recoveryBarrier.KeyReleased {
		t.Fatal("fresh recovery/fencing evidence incomplete")
	}
	for _, n := range recoveryBarrier.Effects {
		if n != 0 {
			t.Fatal("recovery attempted an execution effect")
		}
	}
	if authenticated.Load() != registryBefore["authenticated"] || uploads.Load() != registryBefore["uploads"] || manifestReads.Load() != registryBefore["manifestReads"] {
		t.Fatal("recovery touched the authenticated registry")
	}
	freshDB, err := sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal("postrecovery fresh pool unavailable")
	}
	defer freshDB.Close()
	readJobs := jobqueue.NewPostgres(freshDB)
	readStore := storepg.NewRunStore(freshDB)
	finalJob, err := readJobs.Get(ctx, first.ID)
	finalRun, e := readStore.Get(ctx, payload.Run.ID)
	finalSecond, se := readJobs.Get(ctx, second.ID)
	if err != nil || e != nil || se != nil || finalJob.Status != jobqueue.StatusSucceeded || finalJob.Attempts != 1 || finalJob.LockedBy != "" || finalRun.Status != model.RunStatusSuccess || finalSecond.Status != jobqueue.StatusCancelled || finalSecond.Attempts != 1 {
		t.Fatal("explicit inspection resolution/second disposal differs")
	}
	if err := nativeLossStageEvidence(finalRun, barrier.Digest); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(finalRun.Source, observedRun.Source) || !reflect.DeepEqual(finalRun.StageRuns, observedRun.StageRuns) {
		t.Fatal("recovery rewrote retained source/stage evidence")
	}
	finalDigest, _ := jobqueue.PayloadDigest(finalJob.Payload)
	finalIntent, err := storepg.NewTriggerStore(freshDB).GetIntent(ctx, intent.ID)
	if finalDigest != digest || err != nil || !reflect.DeepEqual(finalIntent.Receipt, accepted.Receipt) {
		t.Fatal("recovery changed payload/receipt/pins")
	}
	if nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, workload) != containerID || nativeRuntimeCommand(ctx, t, "docker", "exec", containerID, "/server", "probe") != marker {
		t.Fatal("recovery changed the actual runtime")
	}
	finalLogs, finalLogCount := nativeLossDurableLogs(ctx, t, freshDB, payload.Run.ID)
	if finalLogCount != beforeLogCount || !bytes.Equal(beforeLogs, finalLogs) {
		t.Fatal("recovery rewrote durable stage logs or cursor evidence")
	}
	if nativeRuntimeCommand(ctx, t, "docker", "logs", containerID) != containerLogsBefore {
		t.Fatal("recovery changed actual runtime stdout")
	}
	finalJSON, _ := json.Marshal(finalRun)
	var rows []byte
	if err := freshDB.QueryRowContext(ctx, `SELECT row_to_json(r)::text FROM pipeline_runs r WHERE id=$1`, payload.Run.ID).Scan(&rows); err != nil {
		t.Fatal("safe persisted run unavailable")
	}
	var logText string
	if err := freshDB.QueryRowContext(ctx, `SELECT COALESCE(string_agg(line,''),'') FROM stage_log_entries WHERE run_id=$1`, payload.Run.ID).Scan(&logText); err != nil {
		t.Fatal("durable log scan unavailable", err)
	}
	safe := bytes.Join([][]byte{first.Payload, finalJob.Payload, beforeJSON, finalJSON, rows, []byte(logText), childLogs.Bytes(), recoveryLogs.Bytes(), []byte(containerLogsBefore)}, []byte("\n"))
	for _, canary := range []string{password, username + ":" + password, base64.StdEncoding.EncodeToString([]byte(username + ":" + password)), base, base64.StdEncoding.EncodeToString(codecKey), hex.EncodeToString(codecKey), "synthetic-fixture-connection-value"} {
		if bytes.Contains(safe, []byte(canary)) {
			t.Fatal("credential/private path escaped into safe evidence")
		}
	}
	evidence := map[string]any{"scope": "owned-native-after-readiness-sigkill-v1", "commit": commit, "secondCommit": secondCommit, "firstJobId": first.ID, "secondJobId": second.ID, "runId": payload.Run.ID, "payloadDigest": digest, "receipt": accepted.Receipt, "source": finalRun.Source, "ociDigest": barrier.Digest, "firstWorker": barrier, "recoveryWorker": recoveryBarrier, "killedBy": "SIGKILL", "registryBeforeKill": registryBefore, "registryAfterRecovery": map[string]int32{"authenticated": authenticated.Load(), "uploads": uploads.Load(), "manifestReads": manifestReads.Load()}, "containerId": containerID, "finalJobStatus": finalJob.Status, "finalRunStatus": finalRun.Status, "secondStatus": finalSecond.Status, "secondAttempts": finalSecond.Attempts, "retainedStageEvidence": true, "stageLogEntries": beforeLogCount, "stageLogRecordsSHA256": hex.EncodeToString(beforeLogHash[:]), "containerStdoutVerified": true, "noPrivateData": true}
	if path := os.Getenv("COOKER_NATIVE_RUNTIME_EVIDENCE"); path != "" {
		data, _ := json.MarshalIndent(evidence, "", "  ")
		if os.WriteFile(path, append(data, '\n'), 0600) != nil {
			t.Fatal("safe evidence persistence failed")
		}
	}
}

// This struct is private IPC only: keys, source directories and control endpoints
// never enter argv, environment, persisted payload or public evidence.
type nativeLossChildInput struct {
	DSN, Nonce, Schema, Mode, JobID, SecondJobID, RunID, PipelineID, RegistryID, IntentID string
	Key                                                                                   []byte
	ParentPID                                                                             int
	Repository, Base, RepositoryID, Buildkit, Marker, Workload, Network                   string
	OldOwner, Image, Digest                                                               string
	BeforeRun                                                                             *model.PipelineRun
}
type nativeLossChildEvidence struct {
	PID, ParentPID                                                               int
	ClaimJobID, ClaimOwner                                                       string
	ClaimAttempts                                                                int
	Image, Digest, DockerHealth                                                  string
	Builds, Pushes, Deploys, SourceOperations                                    int32
	Effects                                                                      map[string]int32 `json:"effects,omitempty"`
	BlockedSecond, StaleCompleteFenced, StaleProgressFenced, StaleTerminalFenced bool
	ResolutionRollback, ResolvedWithoutEffects, KeyReleased                      bool
	AttentionJobStatus                                                           jobqueue.Status
	AttentionRunStatus                                                           model.RunStatus
	FailureCategory                                                              string
	FailurePhases                                                                []string
	PanicFunctions                                                               []string
	Metadata                                                                     map[string]int32
}
type nativeLossProcess struct {
	cmd     *exec.Cmd
	in, out *os.File
	waited  bool
}

func (p *nativeLossProcess) close() {
	if p.cmd.Process != nil && !p.waited {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
		p.waited = true
	}
	_ = p.in.Close()
	_ = p.out.Close()
}
func nativeLossStart(ctx context.Context, t *testing.T, input nativeLossChildInput, base string) (*nativeLossProcess, nativeLossChildEvidence, *bytes.Buffer) {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal("private child input unavailable")
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal("private child result unavailable")
	}
	home, config := filepath.Join(base, "home-"+input.Mode), filepath.Join(base, "docker-"+input.Mode)
	if os.Mkdir(home, 0700) != nil || os.Mkdir(config, 0700) != nil {
		t.Fatal("isolated child profile unavailable")
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeSourceProcessLossChild$", "-test.v", "-test.timeout=120s")
	cmd.Dir = base
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home, "DOCKER_CONFIG=" + config, "TMPDIR=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "COOKER_NATIVE_LOSS_CHILD=private-ipc-v1", "COOKER_OUTPUTS_ENABLED=true"}
	cmd.ExtraFiles = []*os.File{inR, outW}
	logs := &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = logs, logs
	p := &nativeLossProcess{cmd: cmd, in: inW, out: outR}
	if cmd.Start() != nil {
		t.Fatal("new OS worker start failed")
	}
	_ = inR.Close()
	_ = outW.Close()
	if json.NewEncoder(inW).Encode(input) != nil {
		p.close()
		t.Fatal("private child control failed")
	}
	_ = inW.Close()
	var result nativeLossChildEvidence
	done := make(chan error, 1)
	go func() { done <- json.NewDecoder(io.LimitReader(outR, 65536)).Decode(&result) }()
	select {
	case err := <-done:
		if err != nil {
			p.close()
			t.Fatal("new OS worker barrier unavailable; raw output withheld")
		}
	case <-ctx.Done():
		p.close()
		t.Fatal("new OS worker deadline")
	}
	return p, result, logs
}
func nativeLossStageEvidence(run *model.PipelineRun, digest string) error {
	if run == nil || len(run.StageRuns) != 3 {
		return errors.New("started authored stages unavailable")
	}
	seen := map[string]bool{}
	for _, s := range run.StageRuns {
		seen[s.StageID] = true
		switch s.StageID {
		case "build", "push":
			if s.Status != model.RunStatusSuccess || s.StartedAt == nil || s.FinishedAt == nil || s.Outputs["digest"] != digest {
				return errors.New("completed build/push digest evidence differs")
			}
		case "deploy":
			if s.Status != model.RunStatusRunning || s.StartedAt == nil || s.FinishedAt != nil || len(s.Outputs) != 0 || len(s.Artifacts) != 0 {
				return errors.New("withheld deploy result was incorrectly inferred")
			}
		default:
			return errors.New("unknown authored stage")
		}
	}
	if !seen["build"] || !seen["push"] || !seen["deploy"] {
		return errors.New("expected authored stages absent")
	}
	return nil
}

func TestNativeSourceProcessLossChild(t *testing.T) {
	if os.Getenv("COOKER_NATIVE_LOSS_CHILD") != "private-ipc-v1" {
		t.Skip("private OS child fixture required")
	}
	in, out := os.NewFile(3, "private-loss-input"), os.NewFile(4, "private-loss-result")
	defer in.Close()
	defer out.Close()
	var input nativeLossChildInput
	if json.NewDecoder(io.LimitReader(in, 65536)).Decode(&input) != nil || len(input.Key) != 32 || input.ParentPID != os.Getppid() || input.Mode != "execute" && input.Mode != "recover" {
		t.Fatal("private worker control invalid")
	}
	defer clear(input.Key)
	nonce, err := uuid.Parse(input.Nonce)
	if err != nil || nonce == uuid.Nil || nonce.String() != input.Nonce || input.Schema != "native_loss_"+strings.ReplaceAll(input.Nonce, "-", "") || input.Network != "cooker-native-loss-"+input.Nonce+"-net" || input.Workload != "cooker-native-loss-"+input.Nonce+"-web" {
		t.Fatal("worker resource ownership invalid")
	}
	dsn, err := url.Parse(input.DSN)
	if err != nil || dsn.Query().Get("search_path") != input.Schema {
		t.Fatal("worker schema invalid")
	}
	check := *dsn
	q := check.Query()
	q.Del("search_path")
	check.RawQuery = q.Encode()
	if _, err := nativeRuntimeOwnedDatabaseURL(check.String()); err != nil {
		t.Fatal("worker database invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	st, err := storepg.NewStore(ctx, input.DSN)
	if err != nil {
		t.Fatal("fresh worker primary pool failed")
	}
	defer st.Close()
	db, err := sql.Open("postgres", input.DSN)
	if err != nil || db.PingContext(ctx) != nil {
		t.Fatal("fresh worker queue pool failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	codec, err := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(input.Key))
	if err != nil {
		t.Fatal("fresh worker codec failed")
	}
	jobs := jobqueue.NewPostgres(db)
	counters := &freshRevocationCounters{}
	envs := &freshRevocationEnvironments{EnvironmentStore: storepg.NewEnvironmentStore(db), counters: counters}
	registries := &freshRevocationRegistries{RegistryConfigStore: storepg.NewRegistryConfigStore(db), counters: counters}
	manager := &nativeLossManager{Manager: database.New(envs, codec), counters: counters}
	authority := &RegistryAuthorityService{Registries: registries, Environments: envs, Secrets: manager}
	st.Environments = envs
	connections := &connection.Service{Store: &freshRevocationConnections{Store: &connection.Postgres{DB: db}, counters: counters}, Codec: codec}
	source := &nativeRuntimeGit{root: input.Repository, base: input.Base, repositoryID: input.RepositoryID}
	var sourceAPI PipelineSourceProvider = source
	if input.Mode == "recover" {
		sourceAPI = &freshRevocationSource{counters: counters}
	}
	preparer := &PipelineSourcePreparer{Store: st, Connections: connections, Source: sourceAPI}
	jobs.OnEnqueue = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		counters.repin.Add(1)
		if err := storepg.SyncRegistryAuthorityAdmission(ctx, tx, j); err != nil {
			return err
		}
		return storepg.SyncTriggerAdmission(ctx, tx, j)
	}
	jobs.OnTransition = storepg.SyncJobRun
	// Production authority stores are deliberately bound to this fresh queue pool.
	if !storepg.RegistryAdmissionStoresMatch(jobs, registries.RegistryConfigStore, envs.EnvironmentStore) {
		t.Fatal("fresh authority queue-pool binding differs")
	}
	a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, Jobs: jobs, RequireRegistryPreflight: true, RegistryPreflight: func(ctx context.Context, p *model.Pipeline) error {
		return CheckPipelineRegistryPreflight(ctx, authority, p)
	}, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }}
	a.Enqueue = func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
		counters.repin.Add(1)
		return nil, errors.New("worker cannot replace accepted work")
	}
	result := nativeLossChildEvidence{PID: os.Getpid(), ParentPID: os.Getppid()}
	var buildAPI builder.Builder = &freshRevocationBuild{counters: counters}
	var pushAPI pusher.Pusher = &freshRevocationPush{counters: counters}
	var deployAPI deployer.Deployer = &freshRevocationDeploy{counters: counters}
	var realBuild *nativeRuntimeBuild
	var realPush *nativeRuntimePush
	if input.Mode == "execute" {
		realBuild = &nativeRuntimeBuild{delegate: builder.NewBuildKit(input.Buildkit), marker: input.Marker}
		realPush = &nativeRuntimePush{delegate: pusher.NewCrane()}
		buildAPI, pushAPI = realBuild, realPush
		deployAPI = &nativeLossDeploy{delegate: deployer.NewDockerRun(), input: input, result: &result, out: out, build: realBuild, push: realPush, source: source}
	}
	executor := NewExecutor(WithRegistryAuthority(authority), WithEnvironmentResolver(&AppEnvResolver{Environments: envs, Secrets: manager}), WithBuilder(buildAPI), WithPusher(pushAPI), WithDockerDeployer(deployAPI), WithLogStore(logstore.NewPostgres(db, logstore.Limits{})), WithRunUpdater(func(ctx context.Context, run *model.PipelineRun) error {
		counters.stageProgress.Add(1)
		return st.Runs.UpdateProgress(ctx, run.ID, run.StageRuns)
	}))
	runner := NewJobQueueRunner(st, executor, nil)
	runner.SourcePreparer, runner.NativeAutomation, runner.RegistryAuthority = preparer, a, authority
	dispatch := jobqueue.NewRegistry()
	dispatch.Register(JobKindPipelineRun, runner.Handle)
	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{Store: jobs, Registry: dispatch, Size: 1, PollEvery: 25 * time.Millisecond, WorkerIDPrefix: "owned-native-loss-" + input.Mode, LeaseTimeout: 4 * time.Second, HeartbeatEvery: 500 * time.Millisecond, OnHeartbeat: HeartbeatJobRun(st), OnRecovery: RecoverJobRun(st), ClaimContext: func(ctx context.Context, j *jobqueue.Job) context.Context {
		result.ClaimJobID, result.ClaimOwner, result.ClaimAttempts = j.ID, j.LockedBy, j.Attempts
		return store.WithRunClaim(ctx, j.ID, j.LockedBy, nil)
	}})
	if err != nil {
		t.Fatal("genuine fresh worker Pool unavailable")
	}
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- pool.Run(workerCtx) }()
	if input.Mode == "execute" {
		for {
			j, e := jobs.Get(ctx, input.JobID)
			if e != nil {
				t.Fatal("first claim observation unavailable")
			}
			if j.Status == jobqueue.StatusFailed || j.Status == jobqueue.StatusNeedsAttention {
				stop()
				<-done
				result.FailureCategory = "first-outcome-before-runtime-barrier-" + string(j.Status)
				if r, e := st.Runs.Get(ctx, input.RunID); e == nil {
					for _, word := range []string{"source", "registry", "claim", "authority", "environment", "secret", "persist", "pipeline", "context", "deadline", "checkout", "connection", "invalid", "unavailable"} {
						if strings.Contains(strings.ToLower(r.Error), word) {
							result.FailurePhases = append(result.FailurePhases, word)
						}
					}
				}
				capture := freshRevocationChildOut{}
				freshRevocationCapture(&capture, counters)
				result.Effects = capture.Effects
				result.Metadata = capture.Metadata
				result.Builds, result.Pushes, result.SourceOperations = realBuild.calls.Load(), realPush.calls.Load(), source.calls.Load()
				if json.NewEncoder(out).Encode(result) != nil {
					t.Fatal("first outcome diagnostic IPC failed")
				}
				return
			}
			select {
			case <-ctx.Done():
				stop()
				<-done
				t.Fatal("first worker barrier deadline")
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	for {
		job, err := jobs.Get(ctx, input.JobID)
		if err != nil {
			t.Fatal("recovery claim unavailable")
		}
		if job.Status == jobqueue.StatusNeedsAttention {
			break
		}
		if job.Status != jobqueue.StatusRunning {
			t.Fatal("recovery replayed or inferred terminal outcome")
		}
		select {
		case <-ctx.Done():
			stop()
			<-done
			t.Fatal("lease recovery deadline")
		case <-time.After(25 * time.Millisecond):
		}
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal("recovery Pool drain failed")
	}
	recovered, err := jobs.Get(ctx, input.JobID)
	run, e := st.Runs.Get(ctx, input.RunID)
	second, se := jobs.Get(ctx, input.SecondJobID)
	if err != nil || e != nil || se != nil || recovered.Status != jobqueue.StatusNeedsAttention || recovered.Attempts != 1 || recovered.LockedBy != "" || run.Status != model.RunStatusNeedsAttention || run.FinishedAt != nil || second.Status != jobqueue.StatusPending || second.Attempts != 0 {
		t.Fatal("recovery did not retain ambiguity/key")
	}
	result.AttentionJobStatus, result.AttentionRunStatus = recovered.Status, run.Status
	if !reflect.DeepEqual(run.Source, input.BeforeRun.Source) || !reflect.DeepEqual(run.StageRuns, input.BeforeRun.StageRuns) {
		t.Fatal("lease recovery rewrote source/stage evidence")
	}
	if err := nativeLossStageEvidence(run, input.Digest); err != nil {
		t.Fatal(err)
	}
	blocked, err := jobs.Dequeue(ctx, "owned-block-control", []string{JobKindPipelineRun})
	if err != nil || blocked != nil {
		t.Fatal("same-key second job was not blocked")
	}
	result.BlockedSecond = true
	stale := store.WithRunClaim(ctx, input.JobID, input.OldOwner, nil)
	if err := jobs.Complete(ctx, input.OldOwner, input.JobID); !errors.Is(err, jobqueue.ErrNotLocked) {
		t.Fatal("stale Complete regained claim")
	}
	result.StaleCompleteFenced = true
	if err := st.Runs.UpdateProgress(stale, input.RunID, run.StageRuns); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale progress regained claim")
	}
	result.StaleProgressFenced = true
	terminal := run.Clone()
	terminal.Status = model.RunStatusSuccess
	now := time.Now().UTC()
	terminal.FinishedAt = &now
	if err := st.Runs.Update(stale, terminal); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale terminal write regained claim")
	}
	result.StaleTerminalFenced = true
	evidence := "Owned runtime inspected: exact accepted digest " + input.Digest + " and run " + input.RunID + " served its accepted Git marker; deployment result was not acknowledged."
	beforeJobJSON, _ := json.Marshal(recovered)
	beforeRunJSON, _ := json.Marshal(run)
	faultJobs := jobqueue.NewPostgres(db)
	faultJobs.OnTransition = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		if err := storepg.SyncJobRun(ctx, tx, j); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "SELECT 1/0")
		return err
	}
	if err := faultJobs.Resolve(ctx, input.JobID, jobqueue.StatusSucceeded, evidence); err == nil {
		t.Fatal("resolution fault unexpectedly committed")
	}
	rollbackJob, _ := jobs.Get(ctx, input.JobID)
	rollbackRun, _ := st.Runs.Get(ctx, input.RunID)
	jJSON, _ := json.Marshal(rollbackJob)
	rJSON, _ := json.Marshal(rollbackRun)
	if !bytes.Equal(beforeJobJSON, jJSON) || !bytes.Equal(beforeRunJSON, rJSON) {
		t.Fatal("resolution transaction failure split job/domain outcome")
	}
	result.ResolutionRollback = true
	if got := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{index .Config.Labels "io.cooker.run-id"}}`, input.Workload); got != input.RunID || nativeRuntimeCommand(ctx, t, "docker", "exec", input.Workload, "/server", "probe") != input.Marker || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Config.Image}}`, input.Workload) != input.Image {
		t.Fatal("fresh recovery inspection differs")
	}
	if err := jobs.Resolve(ctx, input.JobID, jobqueue.StatusSucceeded, evidence); err != nil {
		t.Fatal("explicit observed resolution rejected", err)
	}
	result.ResolvedWithoutEffects = true
	// Direct secondary claim is only a key-release control after Pool has stopped;
	// its handler is never dispatched, and it is explicitly disposed as cancelled.
	released, err := jobs.Dequeue(ctx, "owned-secondary-disposal", []string{JobKindPipelineRun})
	if err != nil || released == nil || released.ID != input.SecondJobID || released.Attempts != 1 {
		t.Fatal("explicit resolution did not release the key")
	}
	result.KeyReleased = true
	if status, err := jobs.Cancel(ctx, released.ID); err != nil || status != jobqueue.StatusNeedsAttention {
		t.Fatal("undispatched secondary disposal unavailable")
	}
	if err := jobs.Resolve(ctx, released.ID, jobqueue.StatusCancelled, "Owned secondary claim was only a key-release control; its handler never ran and no runtime effects occurred."); err != nil {
		t.Fatal("secondary disposal resolution failed")
	}
	capture := freshRevocationChildOut{}
	freshRevocationCapture(&capture, counters)
	result.Effects = capture.Effects
	// stageProgress records only actual executor flushes; none is allowed during
	// recovery because the same-key job must never reach the registered handler.
	for _, n := range result.Effects {
		if n != 0 {
			t.Fatal("recovery attempted source/secret/build/push/deploy effects")
		}
	}
	if json.NewEncoder(out).Encode(result) != nil {
		t.Fatal("safe recovery evidence IPC failed")
	}
}

type nativeLossManager struct {
	secrets.Manager
	counters *freshRevocationCounters
}

func (*nativeLossManager) EnvironmentGenerationBound() bool { return true }
func (m *nativeLossManager) Get(ctx context.Context, id, key string) ([]byte, error) {
	m.counters.secretGet.Add(1)
	return m.Manager.Get(ctx, id, key)
}
func (m *nativeLossManager) List(ctx context.Context, id string) ([]string, error) {
	m.counters.secretList.Add(1)
	return m.Manager.List(ctx, id)
}
func (m *nativeLossManager) Pin(ctx context.Context, id, key string) (secrets.VersionReference, error) {
	m.counters.secretPin.Add(1)
	if v, ok := m.Manager.(secrets.VersionedManager); ok {
		return v.Pin(ctx, id, key)
	}
	return secrets.VersionReference{}, secrets.ErrVersionUnsupported
}
func (m *nativeLossManager) GetPinned(ctx context.Context, id, key string, ref secrets.VersionReference) ([]byte, error) {
	m.counters.secretPinned.Add(1)
	if v, ok := m.Manager.(secrets.VersionedManager); ok {
		return v.GetPinned(ctx, id, key, ref)
	}
	return nil, secrets.ErrVersionUnsupported
}

type nativeLossDeploy struct {
	delegate deployer.Deployer
	input    nativeLossChildInput
	result   *nativeLossChildEvidence
	out      io.Writer
	build    *nativeRuntimeBuild
	push     *nativeRuntimePush
	source   *nativeRuntimeGit
}

func (d *nativeLossDeploy) Deploy(ctx context.Context, req deployer.Request) (deployer.Result, error) {
	if req.Kind != deployer.KindDockerRun || req.Name != d.input.Workload || req.RegistryAuth == nil || !strings.Contains(req.Image, "@sha256:") || len(req.Env) != 0 || len(req.Ports) != 0 || nativeRuntimeExists(ctx, "container", req.Name) || nativeRuntimeExists(ctx, "image", req.Image) {
		return deployer.Result{}, errors.New("refusing unowned/preloaded lost-worker runtime")
	}
	if req.Labels == nil {
		req.Labels = map[string]string{}
	}
	req.Labels["cooker.fixture"], req.Labels["cooker.agent"], req.Network = d.input.Nonce, "catalog_review", d.input.Network
	result, err := d.delegate.Deploy(ctx, req)
	if err != nil {
		return result, err
	}
	for {
		state, err := exec.CommandContext(ctx, "docker", "inspect", "--format", `{{.State.Health.Status}}`, d.input.Workload).Output()
		if err != nil {
			return result, errors.New("owned readiness inspection failed")
		}
		if strings.TrimSpace(string(state)) == "healthy" {
			break
		}
		if strings.TrimSpace(string(state)) == "unhealthy" {
			return result, errors.New("owned runtime unhealthy")
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	marker, err := exec.CommandContext(ctx, "docker", "exec", d.input.Workload, "/server", "probe").Output()
	if err != nil || strings.TrimSpace(string(marker)) != d.input.Marker {
		return result, errors.New("owned runtime marker differs")
	}
	d.result.Image = req.Image
	d.result.Digest = strings.Split(req.Image, "@")[1]
	d.result.DockerHealth = "healthy"
	d.result.Builds, d.result.Pushes, d.result.Deploys, d.result.SourceOperations = d.build.calls.Load(), d.push.calls.Load(), 1, d.source.calls.Load()
	if json.NewEncoder(d.out).Encode(d.result) != nil {
		return result, errors.New("owned readiness barrier IPC failed")
	}
	// The result is deliberately withheld. Parent SIGKILL terminates this actual
	// OS worker while Pool still owns the claim and can heartbeat normally.
	<-ctx.Done()
	return result, ctx.Err()
}

// Production Pool beats before dispatch. Modern native admission carries the
// immutable pending run, and the actual runner reconstructs its view; that
// permitted creation race must not be mistaken for lost execution ownership.
func TestNativeSourceHeartbeatAcceptsPendingSnapshotCreationRace(t *testing.T) {
	preparer, p, _ := nativeSourceSetup(t)
	ctx := context.Background()
	jobs := jobqueue.NewMemory()
	producer := NewJobQueueEnqueuer(jobs, preparer.Store.Pipelines)
	producer.AdmissionGuard = memory.NewTriggerStore()
	producer.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
		return preparer.Prepare(ctx, p, p.Source.FullRef, "manual")
	}
	run := pendingPipelineRun(p, "native-heartbeat-creation-race")
	if err := producer.EnqueuePlannedRun(ctx, p, run); err != nil {
		t.Fatal("actual native admission failed", err)
	}
	if _, err := preparer.Store.Runs.Get(ctx, run.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("native pending view must not already exist")
	}
	j, err := jobs.Dequeue(ctx, "owned-native-first-beat", []string{JobKindPipelineRun})
	if err != nil || j == nil || j.Attempts != 1 {
		t.Fatal("actual native claim unavailable")
	}
	var accepted runJobPayload
	if json.Unmarshal(j.Payload, &accepted) != nil || accepted.Run == nil || accepted.Source == nil || accepted.Run.Source == nil || accepted.Run.Source.CheckedOutAt != nil || ValidatePipelineSourcePlan(accepted.Pipeline, accepted.Source) != nil {
		t.Fatal("accepted reconstruction evidence invalid")
	}
	claim := store.WithRunClaim(ctx, j.ID, j.LockedBy, func(c context.Context, write func() error) error { return jobs.WithClaim(c, j.LockedBy, j.ID, write) })
	if err := HeartbeatJobRun(preparer.Store)(claim, j); err != nil {
		t.Fatal("first heartbeat cancelled a valid native reconstruction race", err)
	}
}

func TestNativeSourceHeartbeatMissingViewDeniesContradictorySnapshots(t *testing.T) {
	preparer, p, _ := nativeSourceSetup(t)
	ctx := context.Background()
	jobs := jobqueue.NewMemory()
	producer := NewJobQueueEnqueuer(jobs, preparer.Store.Pipelines)
	producer.AdmissionGuard = memory.NewTriggerStore()
	producer.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
		return preparer.Prepare(ctx, p, p.Source.FullRef, "manual")
	}
	run := pendingPipelineRun(p, "native-heartbeat-negative-matrix")
	if producer.EnqueuePlannedRun(ctx, p, run) != nil {
		t.Fatal("native matrix admission unavailable")
	}
	j, err := jobs.Dequeue(ctx, "owned-heartbeat-matrix", []string{JobKindPipelineRun})
	if err != nil || j == nil {
		t.Fatal("native matrix claim unavailable")
	}
	cases := []struct {
		name   string
		mutate func(*runJobPayload, *jobqueue.Job)
	}{
		{"absent-pipeline", func(p *runJobPayload, j *jobqueue.Job) { p.Pipeline = nil }},
		{"absent-run", func(p *runJobPayload, j *jobqueue.Job) { p.Run = nil }},
		{"legacy-ids-only", func(p *runJobPayload, j *jobqueue.Job) { p.Pipeline = nil; p.Run = nil; p.Source = nil }},
		{"wrong-job-identity", func(p *runJobPayload, j *jobqueue.Job) { j.ID = "run:foreign" }},
		{"wrong-snapshot-pipeline", func(p *runJobPayload, j *jobqueue.Job) { p.Pipeline.ID = "foreign" }},
		{"wrong-snapshot-run", func(p *runJobPayload, j *jobqueue.Job) { p.Run.ID = "foreign" }},
		{"wrong-parent", func(p *runJobPayload, j *jobqueue.Job) { p.Run.PipelineID = "foreign" }},
		{"wrong-version", func(p *runJobPayload, j *jobqueue.Job) { p.Run.PipelineVersion++ }},
		{"zero-version", func(p *runJobPayload, j *jobqueue.Job) { p.Run.PipelineVersion = 0; p.Pipeline.Version = 0 }},
		{"nonpending-run", func(p *runJobPayload, j *jobqueue.Job) { p.Run.Status = model.RunStatusRunning }},
		{"missing-created-at", func(p *runJobPayload, j *jobqueue.Job) { p.Run.CreatedAt = time.Time{} }},
		{"started-run", func(p *runJobPayload, j *jobqueue.Job) { now := time.Now().UTC(); p.Run.StartedAt = &now }},
		{"finished-run", func(p *runJobPayload, j *jobqueue.Job) { now := time.Now().UTC(); p.Run.FinishedAt = &now }},
		{"run-error", func(p *runJobPayload, j *jobqueue.Job) { p.Run.Error = "prior attempt" }},
		{"stage-running", func(p *runJobPayload, j *jobqueue.Job) { p.Run.StageRuns[0].Status = model.RunStatusRunning }},
		{"stage-started", func(p *runJobPayload, j *jobqueue.Job) { now := time.Now().UTC(); p.Run.StageRuns[0].StartedAt = &now }},
		{"stage-logs", func(p *runJobPayload, j *jobqueue.Job) { p.Run.StageRuns[0].Logs = "prior effect" }},
		{"stage-output", func(p *runJobPayload, j *jobqueue.Job) {
			p.Run.StageRuns[0].Outputs = map[string]string{"digest": "prior"}
		}},
		{"wrong-stage", func(p *runJobPayload, j *jobqueue.Job) { p.Run.StageRuns[0].StageID = "foreign" }},
		{"missing-stage", func(p *runJobPayload, j *jobqueue.Job) { p.Run.StageRuns = nil }},
		{"missing-source-plan", func(p *runJobPayload, j *jobqueue.Job) { p.Source = nil }},
		{"missing-source-receipt", func(p *runJobPayload, j *jobqueue.Job) { p.Run.Source = nil }},
		{"checkout-claim", func(p *runJobPayload, j *jobqueue.Job) { now := time.Now().UTC(); p.Run.Source.CheckedOutAt = &now }},
		{"wrong-source-lineage", func(p *runJobPayload, j *jobqueue.Job) { p.Run.Source.CommitSHA = strings.Repeat("b", 40) }},
		{"automatic-without-trigger-proof", func(p *runJobPayload, j *jobqueue.Job) {
			p.Source.ReviewPolicy = "automatic"
			p.Source.PlanHash = model.PipelineSourceConfigDigest(p.Pipeline, p.Source)
			p.Run.Source, _ = model.RunSourceFromAcceptedPlan(p.Source)
			p.Trigger = nil
		}},
		{"invalid-source-plan", func(p *runJobPayload, j *jobqueue.Job) { p.Source.Version = 2 }},
		{"imported-pipeline", func(p *runJobPayload, j *jobqueue.Job) {
			p.Pipeline.ID = "vflow_readonly"
			p.PipelineID = p.Pipeline.ID
			p.Run.PipelineID = p.Pipeline.ID
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copyJob := *j
			var accepted runJobPayload
			if json.Unmarshal(j.Payload, &accepted) != nil {
				t.Fatal("native matrix copy invalid")
			}
			tc.mutate(&accepted, &copyJob)
			copyJob.Payload, _ = json.Marshal(accepted)
			claim := store.WithRunClaim(ctx, j.ID, j.LockedBy, func(c context.Context, write func() error) error { return jobs.WithClaim(c, j.LockedBy, j.ID, write) })
			if err := HeartbeatJobRun(preparer.Store)(claim, &copyJob); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("contradictory missing snapshot was tolerated")
			}
		})
	}
	t.Run("malformed-json", func(t *testing.T) {
		bad := *j
		bad.Payload = json.RawMessage(`{"runId":"native-heartbeat-negative-matrix","pipeline":true}`)
		if err := HeartbeatJobRun(preparer.Store)(ctx, &bad); err == nil {
			t.Fatal("malformed missing snapshot was tolerated")
		}
	})
	t.Run("non-not-found", func(t *testing.T) {
		sentinel := errors.New("synthetic store failure for identity propagation")
		copyStore := *preparer.Store
		copyStore.Runs = &nativeLossHeartbeatErrorStore{RunStore: preparer.Store.Runs, err: sentinel}
		if err := HeartbeatJobRun(&copyStore)(ctx, j); err != sentinel {
			t.Fatal("real store error was suppressed or rewritten")
		}
	})
}

type nativeLossHeartbeatErrorStore struct {
	store.RunStore
	err error
}

func (s *nativeLossHeartbeatErrorStore) UpdateHeartbeat(context.Context, string, time.Time) error {
	return s.err
}

func nativeLossDurableLogs(ctx context.Context, t *testing.T, db *sql.DB, runID string) ([]byte, int) {
	t.Helper()
	var data []byte
	var count int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(json_agg(json_build_object('stageId',stage_id,'seq',seq,'timestamp',timestamp,'line',line) ORDER BY stage_id,seq),'[]'::json)::text,count(*) FROM stage_log_entries WHERE run_id=$1`, runID).Scan(&data, &count)
	if err != nil {
		t.Fatal("durable stage log evidence unavailable")
	}
	return data, count
}
