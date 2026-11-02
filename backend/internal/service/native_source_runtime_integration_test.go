//go:build integration

package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/pusher"
	cookercrypto "github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// This opt-in test needs the labelled, disposable harness recorded under
// registry-authority/native-runtime. It executes real local Git, BuildKit OCI
// export, authenticated registry upload and Docker pull/run. Hosted identity is
// a fixture; direct Docker observation is not a native service-observation API.
func TestNativeSourceRegistryGenuineLocalRuntime(t *testing.T) {
	if os.Getenv("COOKER_NATIVE_RUNTIME_ACCEPTANCE") != "owned-local-v1" {
		t.Skip("explicit owned local runtime harness required")
	}
	nonce := os.Getenv("COOKER_NATIVE_RUNTIME_NONCE")
	if u, err := uuid.Parse(nonce); err != nil || u.String() != nonce || u == uuid.Nil {
		t.Fatal("invalid fixture ownership nonce")
	}
	prefix := "cooker-native-runtime-" + nonce
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
	schema := "native_runtime_" + strings.ReplaceAll(nonce, "-", "")
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
	var runtimeDeploy *nativeRuntimeDeploy
	defer func() {
		ref := ""
		if runtimeDeploy != nil {
			ref = runtimeDeploy.image
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
	build := &nativeRuntimeBuild{delegate: builder.NewBuildKit(buildkitURL.String()), marker: marker}
	push := &nativeRuntimePush{delegate: pusher.NewCrane()}
	deploy := &nativeRuntimeDeploy{delegate: deployer.NewDockerRun(), name: workload, nonce: nonce, network: network, registryReads: &manifestReads}
	runtimeDeploy = deploy
	executor := NewExecutor(WithBuilder(build), WithPusher(push), WithDockerDeployer(deploy), WithRegistryAuthority(authority), WithRunUpdater(func(ctx context.Context, run *model.PipelineRun) error {
		return st.Runs.UpdateProgress(ctx, run.ID, run.StageRuns)
	}))
	producer := NewJobQueueEnqueuer(jobs, st.Pipelines)
	producer.AdmissionGuard = storepg.NewTriggerStore(db)
	producer.RequireRegistryPins = true
	var pins atomic.Int32
	producer.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		pins.Add(1)
		plan, err := PreparePipelineRegistryAuthority(ctx, authority, p)
		if err != nil {
			t.Log("owned registry preparation rejected", err)
		}
		return plan, err
	}
	producer.PrepareSecrets = executor.PreparePipelineSecrets
	a.Enqueue = producer.EnqueueSourceRun
	if err := a.Dispatch(ctx, intent); err != nil {
		t.Fatal("native admission", err)
	}
	accepted, err := a.Triggers.Store.GetIntent(ctx, intent.ID)
	if err != nil || accepted.Receipt == nil {
		t.Fatal("native receipt missing", err)
	}
	job, err := jobs.Get(ctx, accepted.Receipt.JobID)
	if err != nil {
		t.Fatal(err)
	}
	var payload runJobPayload
	if json.Unmarshal(job.Payload, &payload) != nil || payload.Source == nil || payload.Registry == nil || payload.Run == nil || payload.Run.Source == nil || payload.Run.Source.CheckedOutAt != nil || payload.Source.CommitSHA != commit || payload.Registry.Connections[config.ID].Credential.Mode != "database" {
		t.Fatal("admission evidence missing")
	}
	runner := NewJobQueueRunner(st, executor, nil)
	runner.SourcePreparer, runner.NativeAutomation, runner.RegistryAuthority = preparer, a, authority
	registryDispatch := jobqueue.NewRegistry()
	registryDispatch.Register(JobKindPipelineRun, runner.Handle)
	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{Store: jobs, Registry: registryDispatch, Size: 1, PollEvery: 50 * time.Millisecond, WorkerIDPrefix: prefix, LeaseTimeout: 30 * time.Second, HeartbeatEvery: 2 * time.Second, ClaimContext: func(ctx context.Context, job *jobqueue.Job) context.Context {
		return store.WithRunClaim(ctx, job.ID, job.LockedBy, nil)
	}})
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, cancelWorker := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- pool.Run(workerCtx) }()
	workerStopped := false
	defer func() {
		if !workerStopped {
			cancelWorker()
			<-done
		}
	}()
	for {
		job, err = jobs.Get(ctx, accepted.Receipt.JobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == jobqueue.StatusSucceeded {
			break
		}
		if job.Status == jobqueue.StatusFailed || job.Status == jobqueue.StatusNeedsAttention {
			if failed, readErr := st.Runs.Get(ctx, payload.Run.ID); readErr == nil {
				for _, stage := range failed.StageRuns {
					message := strings.ReplaceAll(strings.ReplaceAll(stage.Logs, password, "[redacted]"), base, "[owned-temp]")
					t.Logf("owned failed stage %s: status=%s logs=%s", stage.StageID, stage.Status, message)
				}
			}
			t.Fatalf("genuine native job %s: %s", job.Status, job.LastError)
		}
		select {
		case <-ctx.Done():
			t.Fatal("native runtime deadline")
		case <-time.After(100 * time.Millisecond):
		}
	}
	cancelWorker()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	workerStopped = true
	run, err := st.Runs.Get(ctx, payload.Run.ID)
	if err != nil || run.Status != model.RunStatusSuccess || run.Source == nil || run.Source.CheckedOutAt == nil || run.Source.CommitSHA != commit || run.Source.SourcePlanDigest != payload.Run.Source.SourcePlanDigest {
		t.Fatal("verified run source missing", err)
	}
	if pins.Load() != 1 || build.calls.Load() != 1 || push.calls.Load() != 1 || deploy.calls.Load() != 1 || authenticated.Load() == 0 || uploads.Load() == 0 || manifestReads.Load() == 0 {
		t.Fatal("genuine adapters did not execute once")
	}
	var buildDigest, pushDigest string
	for _, stage := range run.StageRuns {
		if stage.Status != model.RunStatusSuccess || stage.Logs == "" {
			t.Fatalf("stage result/log evidence missing: stage=%s status=%s logs=%d", stage.StageID, stage.Status, len(stage.Logs))
		}
		if stage.StageID == "build" {
			buildDigest = stage.Outputs["digest"]
			if !strings.Contains(stage.Logs, "built "+marker) {
				t.Fatal("actual BuildKit stdout missing")
			}
		}
		if stage.StageID == "push" {
			pushDigest = stage.Outputs["digest"]
		}
	}
	if !strings.HasPrefix(buildDigest, "sha256:") || buildDigest != pushDigest || !strings.HasSuffix(deploy.image, "@"+pushDigest) {
		t.Fatal("OCI push/runtime digest mismatch")
	}
	for {
		state := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.State.Health.Status}}`, workload)
		if state == "healthy" {
			break
		}
		if state == "unhealthy" {
			t.Fatal("owned workload is unhealthy")
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned readiness deadline")
		case <-time.After(200 * time.Millisecond):
		}
	}
	if nativeRuntimeCommand(ctx, t, "docker", "exec", workload, "/server", "probe") != marker {
		t.Fatal("runtime served another Git input")
	}
	containerLogs := nativeRuntimeCommand(ctx, t, "docker", "logs", workload)
	if !strings.Contains(containerLogs, "ready "+marker) {
		t.Fatal("real container stdout missing")
	}
	if nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{index .Config.Labels "io.cooker.run-id"}}`, workload) != run.ID {
		t.Fatal("runtime run identity missing")
	}
	if refs := nativeRuntimeCommand(ctx, t, "docker", "image", "inspect", "--format", `{{json .RepoDigests}}`, deploy.image); !strings.Contains(refs, "@"+pushDigest) {
		t.Fatal("Docker image digest missing")
	}
	for _, root := range source.checkouts {
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("source checkout retained")
		}
	}
	for _, root := range build.contexts {
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("source export retained")
		}
	}
	encodedRun, _ := json.Marshal(run)
	var storedRun string
	if err := db.QueryRowContext(ctx, `SELECT row_to_json(r)::text FROM pipeline_runs r WHERE id=$1`, run.ID).Scan(&storedRun); err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{password, username + ":" + password, base64.StdEncoding.EncodeToString([]byte(username + ":" + password)), base, "synthetic-fixture-connection-value"} {
		if strings.Contains(string(job.Payload)+string(encodedRun)+storedRun+containerLogs, canary) {
			t.Fatal("private data escaped to safe runtime evidence")
		}
	}
	// Receipt-first recovery wins over current revoked grants and unavailable
	// provider/preflight callbacks; no new source, secret or runtime preparation.
	if _, err := st.Registries.(interface {
		UpdateGrants(context.Context, string, int64, []string, []string) (*model.RegistryConfig, error)
	}).UpdateGrants(ctx, config.ID, config.Version, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := source.calls.Load()
	var recoveredAdmissions atomic.Int32
	reopenedJobs := jobqueue.NewPostgres(db)
	reopenedJobs.OnEnqueue, reopenedJobs.OnTransition = jobs.OnEnqueue, jobs.OnTransition
	reopened := &PipelineAutomation{Jobs: reopenedJobs, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, RequireRegistryPreflight: true, Enqueue: func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
		recoveredAdmissions.Add(1)
		return nil, errors.New("owned preparation is unavailable")
	}}
	for i := 0; i < 3; i++ {
		if err := reopened.Dispatch(ctx, intent); err != nil {
			t.Fatal("receipt recovery", err)
		}
	}
	if recoveredAdmissions.Load() != 0 || source.calls.Load() != before || pins.Load() != 1 || build.calls.Load() != 1 || push.calls.Load() != 1 || deploy.calls.Load() != 1 {
		t.Fatal("receipt recovery repeated an effect")
	}
	recovered, err := reopened.Triggers.Store.GetIntent(ctx, intent.ID)
	if err != nil || recovered.Receipt.JobID != accepted.Receipt.JobID || recovered.Receipt.PlanDigest != accepted.Receipt.PlanDigest {
		t.Fatal("recovery changed admission")
	}
	var jobsCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM jobs").Scan(&jobsCount); err != nil || jobsCount != 1 {
		t.Fatal("native job duplication", err)
	}
	evidence := map[string]any{"scope": "owned-local-native-runtime-v1", "commit": commit, "source": run.Source, "jobId": job.ID, "jobStatus": job.Status, "receipt": accepted.Receipt, "runId": run.ID, "ociDigest": pushDigest, "sourceOperations": before, "registryPreparations": pins.Load(), "builds": build.calls.Load(), "pushes": push.calls.Load(), "deploys": deploy.calls.Load(), "authenticatedRequests": authenticated.Load(), "manifestUploads": uploads.Load(), "manifestReads": manifestReads.Load(), "dockerHealth": "healthy", "containerStdoutVerified": true, "receiptRetries": 3, "runtimeContainerId": nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, workload), "runtimeImageId": nativeRuntimeCommand(ctx, t, "docker", "image", "inspect", "--format", `{{.Id}}`, deploy.image), "runtimeImageRef": deploy.image}
	if path := os.Getenv("COOKER_NATIVE_RUNTIME_EVIDENCE"); path != "" {
		data, _ := json.MarshalIndent(evidence, "", "  ")
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("real local Git checkout -> BuildKit OCI -> authenticated registry -> Docker pull/health/log proven; hosted metadata and native observation API remain unclaimed")
}

func nativeRuntimeOwnedDatabaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/cooker_native_runtime_test" || u.User == nil || u.User.Username() != "native_runtime_fixture" || u.Query().Get("sslmode") != "disable" || len(u.Query()) != 1 || u.Fragment != "" {
		return nil, errors.New("refusing non-owned runtime database")
	}
	if _, password := u.User.Password(); password {
		return nil, errors.New("runtime database must not read a credential")
	}
	return u, nil
}

func nativeRuntimeCommand(ctx context.Context, t *testing.T, bin string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("owned %s command failed: %v: %s", bin, err, out)
	}
	return strings.TrimSpace(string(out))
}

func nativeRuntimeExists(ctx context.Context, kind, name string) bool {
	return exec.CommandContext(ctx, "docker", kind, "inspect", name).Run() == nil
}

func nativeRuntimeCleanup(t *testing.T, nonce, name, image, knownDigest string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	evidence := map[string]any{"scope": "owned-local-native-runtime-cleanup", "nonce": nonce, "containerName": name}
	if nativeRuntimeExists(ctx, "container", name) {
		id := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, name)
		label := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{index .Config.Labels "cooker.fixture"}}`, id)
		if label != nonce {
			t.Error("refusing unowned runtime cleanup")
			return
		}
		ref := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Config.Image}}`, id)
		nativeRuntimeCommand(ctx, t, "docker", "rm", "-f", id)
		evidence["removedContainerId"], evidence["imageRef"] = id, ref
		if strings.Contains(ref, "/owned/native-"+nonce+"@sha256:") && nativeRuntimeExists(ctx, "image", ref) {
			nativeRuntimeCommand(ctx, t, "docker", "image", "rm", ref)
		}
	}
	if nativeRuntimeExists(ctx, "image", image) {
		nativeRuntimeCommand(ctx, t, "docker", "image", "rm", image)
	}
	if strings.HasPrefix(knownDigest, strings.TrimSuffix(image, ":accepted")+"@sha256:") && nativeRuntimeExists(ctx, "image", knownDigest) {
		nativeRuntimeCommand(ctx, t, "docker", "image", "rm", knownDigest)
	}
	evidence["remainingContainer"] = nativeRuntimeExists(ctx, "container", name)
	evidence["remainingImage"] = knownDigest != "" && nativeRuntimeExists(ctx, "image", knownDigest)
	if path := os.Getenv("COOKER_NATIVE_RUNTIME_EVIDENCE"); path != "" {
		data, _ := json.MarshalIndent(evidence, "", "  ")
		if err := os.WriteFile(path+".cleanup.json", append(data, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}
}

type nativeRuntimeGit struct {
	root, base, repositoryID string
	calls                    atomic.Int32
	checkouts                []string
}

func (s *nativeRuntimeGit) Identity(_ context.Context, c *connection.Connection, repo string) (model.GitSourceIdentity, error) {
	s.calls.Add(1)
	return model.GitSourceIdentity{Provider: c.Provider, ConnectionID: c.ID, Host: "github.com", RepositoryID: s.repositoryID, Repository: repo}, nil
}
func (s *nativeRuntimeGit) Revision(ctx context.Context, _ *connection.Connection, _, ref string) (SourceRevision, error) {
	s.calls.Add(1)
	out, err := exec.CommandContext(ctx, "git", "-C", s.root, "rev-parse", "--verify", ref+"^{commit}").Output()
	commit := strings.TrimSpace(string(out))
	return SourceRevision{FullRef: ref, Commit: commit, Object: commit}, err
}
func (s *nativeRuntimeGit) CheckoutSource(ctx context.Context, plan *model.PipelineSourcePlan, _ io.Writer) (string, error) {
	s.calls.Add(1)
	root, err := os.MkdirTemp(s.base, "checkout-")
	if err != nil {
		return "", err
	}
	s.checkouts = append(s.checkouts, root)
	if err := exec.CommandContext(ctx, "git", "clone", "--no-hardlinks", "--no-checkout", "--", s.root, root).Run(); err != nil {
		os.RemoveAll(root)
		return "", errors.New("owned Git clone failed")
	}
	if err := exec.CommandContext(ctx, "git", "-C", root, "checkout", "--detach", plan.CommitSHA).Run(); err != nil {
		os.RemoveAll(root)
		return "", errors.New("owned exact checkout failed")
	}
	return root, nil
}

type nativeRuntimeBuild struct {
	delegate builder.Builder
	marker   string
	calls    atomic.Int32
	contexts []string
}

func (b *nativeRuntimeBuild) Build(ctx context.Context, req builder.Request) (builder.Result, error) {
	b.calls.Add(1)
	b.contexts = append(b.contexts, req.ContextDir)
	marker, err := os.ReadFile(filepath.Join(req.ContextDir, "message.txt"))
	if err != nil || string(marker) != b.marker {
		return builder.Result{}, errors.New("build did not consume accepted Git source")
	}
	if _, err := os.Stat(filepath.Join(req.ContextDir, ".git")); !os.IsNotExist(err) {
		return builder.Result{}, errors.New("private Git metadata entered OCI context")
	}
	return b.delegate.Build(ctx, req)
}

type nativeRuntimePush struct {
	delegate pusher.Pusher
	calls    atomic.Int32
}

func (p *nativeRuntimePush) Push(ctx context.Context, req pusher.Request) (pusher.Result, error) {
	p.calls.Add(1)
	if req.Artifact == nil || req.RegistryAuth == nil {
		return pusher.Result{}, errors.New("private OCI handoff or registry authority missing")
	}
	return p.delegate.Push(ctx, req)
}

type nativeRuntimeDeploy struct {
	delegate                    deployer.Deployer
	name, nonce, network, image string
	calls                       atomic.Int32
	registryReads               *atomic.Int32
}

func (d *nativeRuntimeDeploy) Deploy(ctx context.Context, req deployer.Request) (deployer.Result, error) {
	d.calls.Add(1)
	if req.Kind != deployer.KindDockerRun || req.Name != d.name || req.RegistryAuth == nil || !strings.Contains(req.Image, "@sha256:") || len(req.Env) != 0 || len(req.Ports) != 0 || nativeRuntimeExists(ctx, "container", req.Name) || nativeRuntimeExists(ctx, "image", req.Image) {
		return deployer.Result{}, errors.New("refusing unowned or preloaded runtime fixture")
	}
	d.image = req.Image
	if req.Labels == nil {
		req.Labels = map[string]string{}
	}
	req.Labels["cooker.fixture"], req.Labels["cooker.agent"], req.Network = d.nonce, "catalog_review", d.network
	before := d.registryReads.Load()
	result, err := d.delegate.Deploy(ctx, req)
	if err == nil && d.registryReads.Load() == before {
		return result, fmt.Errorf("runtime did not pull the authenticated OCI image")
	}
	return result, err
}
