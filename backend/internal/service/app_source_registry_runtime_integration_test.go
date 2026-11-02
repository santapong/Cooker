//go:build integration

package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
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
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// This opt-in test needs the labelled, disposable harness recorded under
// registry-authority/app-runtime. It executes real local Git, BuildKit OCI
// export, authenticated registry upload and Docker pull/run. Hosted identity is
// a fixture; App service observation uses the real generation-bound RuntimeService.
func TestAppSourceRegistryGenuineLocalRuntime(t *testing.T) {
	if os.Getenv("COOKER_APP_RUNTIME_ACCEPTANCE") != "owned-local-v1" {
		t.Skip("explicit owned local runtime harness required")
	}
	nonce := os.Getenv("COOKER_APP_RUNTIME_NONCE")
	if u, err := uuid.Parse(nonce); err != nil || u.String() != nonce || u == uuid.Nil {
		t.Fatal("invalid fixture ownership nonce")
	}
	prefix := "cooker-app-" + strings.ReplaceAll(nonce, "-", "")[:24]
	network := "cooker-app-runtime-" + nonce + "-net"
	if os.Getenv("COOKER_APP_RUNTIME_NETWORK") != network {
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
	buildkitURL, err := url.Parse(os.Getenv("COOKER_APP_RUNTIME_BUILDKIT"))
	if err != nil || buildkitURL.Scheme != "tcp" || buildkitURL.Hostname() != "127.0.0.1" || buildkitURL.Port() == "" || buildkitURL.User != nil || buildkitURL.Path != "" || buildkitURL.RawQuery != "" {
		t.Fatal("unowned BuildKit control endpoint")
	}
	dsn, err := appRuntimeOwnedDatabaseURL(os.Getenv("COOKER_APP_RUNTIME_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "app_runtime_" + strings.ReplaceAll(nonce, "-", "")
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
	password := "app-runtime-private-" + uuid.NewString()
	username := "app-runtime-fixture"
	var authenticated, uploads, manifestReads atomic.Int32
	reg := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != username || p != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="owned-app-fixture"`)
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
	repositoryPrefix := registryHost + "/owned/" + prefix
	workload := prefix
	if nativeRuntimeExists(ctx, "container", workload) || nativeRuntimeExists(ctx, "image", repositoryPrefix+":accepted") {
		t.Fatal("fixture identity already exists")
	}
	var runtimeDeploy *appRuntimeDeploy
	defer func() {
		ref := ""
		if runtimeDeploy != nil {
			ref = runtimeDeploy.image
		}
		appRuntimeCleanup(t, nonce, workload, repositoryPrefix, ref)
	}()

	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	if err := os.Mkdir(repository, 0700); err != nil {
		t.Fatal(err)
	}
	server, err := os.ReadFile(os.Getenv("COOKER_APP_RUNTIME_SERVER_BINARY"))
	if err != nil || len(server) == 0 {
		t.Fatal("owned static App server input unavailable")
	}
	marker := "accepted-app-commit-" + nonce
	for path, contents := range map[string][]byte{
		"server":        server,
		"message.txt":   []byte(marker),
		".dockerignore": []byte(".git\n"),
		"Dockerfile":    []byte("FROM scratch\nCOPY server /server\nCOPY message.txt /message.txt\nRUN [\"/server\",\"build-proof\"]\nHEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=15 CMD [\"/server\",\"probe\"]\nENTRYPOINT [\"/server\"]\n"),
	} {
		if err := os.WriteFile(filepath.Join(repository, path), contents, 0700); err != nil {
			t.Fatal(err)
		}
	}
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "init", "--initial-branch=main")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "add", "Dockerfile", "server", "message.txt", ".dockerignore")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "-c", "user.name=Owned fixture", "-c", "user.email=fixture@invalid.local", "commit", "-m", "owned App runtime input")
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
	c := &connection.Connection{Name: "owned App runtime source", Provider: "github", Enabled: true, Repositories: []string{"owned/app-runtime"}}
	if err := connections.Create(ctx, c, "synthetic-app-fixture-connection-value"); err != nil {
		t.Fatal(err)
	}
	manager := database.New(storepg.NewEnvironmentStore(db), codec)
	appLogCanary := "app-runtime-log-" + uuid.NewString()
	appEnvironmentID := uuid.NewString()
	if err := st.Environments.Create(ctx, &model.Environment{ID: appEnvironmentID, Name: "owned App logs", PlainVars: map[string]string{}, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Put(ctx, appEnvironmentID, "APP_LOG_CANARY", []byte(appLogCanary)); err != nil {
		t.Fatal(err)
	}
	incarnation := uuid.NewString()
	registryEnvironmentID := "registry_" + strings.ReplaceAll(incarnation, "-", "")
	if err := st.Environments.Create(ctx, &model.Environment{ID: registryEnvironmentID, Name: "owned App registry private", PlainVars: map[string]string{}, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Put(ctx, registryEnvironmentID, "password", []byte(password)); err != nil {
		t.Fatal(err)
	}
	app := &model.App{ID: uuid.NewString(), Name: prefix, GitHubRepo: "owned/app-runtime", Branch: "main", EnvironmentID: appEnvironmentID, RegistryRef: registryHost + "/owned", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: prefix}, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile", SourceConnectionID: c.ID, SourceConnectionVersion: c.Version, SourceRepositoryID: "owned-local-app-" + nonce}}
	config := &model.RegistryConfig{ID: uuid.NewString(), Name: "Owned App runtime registry", URL: registryServer.URL, Username: username, PasswordRef: "registry:v2:" + incarnation, AllowedAppIDs: []string{app.ID}}
	if err := st.Registries.Create(ctx, config); err != nil {
		t.Fatal(err)
	}
	app.BuildPlan.RegistryConnectionIDs = []string{config.ID}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	app, err = st.Apps.Get(ctx, app.ID)
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
		t.Fatal("App authority pool mismatch")
	}
	source := &appRuntimeGit{root: repository, base: base, connections: connections.Store, connectionID: c.ID, version: c.Version, repositoryID: app.BuildPlan.SourceRepositoryID, acceptedCommit: commit}
	build := &appRuntimeBuild{delegate: builder.NewBuildKit(buildkitURL.String()), marker: marker, commit: commit}
	push := &nativeRuntimePush{delegate: pusher.NewCrane()}
	deploy := &appRuntimeDeploy{delegate: deployer.NewDockerRun(), name: workload, nonce: nonce, network: network, appCanary: appLogCanary, repository: repositoryPrefix, registryReads: &manifestReads}
	runtimeDeploy = deploy
	stageLogs := logstore.NewPostgres(db, logstore.Limits{})
	executor := NewExecutor(WithBuilder(build), WithPusher(push), WithDockerDeployer(deploy), WithRegistryAuthority(authority), WithLogStore(stageLogs), WithRunUpdater(func(ctx context.Context, run *model.PipelineRun) error {
		return st.Runs.UpdateProgress(ctx, run.ID, run.StageRuns)
	}))
	d := NewAppDeployer(executor, "")
	d.Apps, d.Deploys = st.Apps, st.AppDeploys
	d.EnvResolver = &AppEnvResolver{Environments: storepg.NewEnvironmentStore(db), Secrets: manager}
	d.SaveRun = st.Runs.Update
	d.SavePipeline = func(ctx context.Context, p *model.Pipeline) error {
		old, err := st.Pipelines.Get(ctx, p.ID)
		if errors.Is(err, store.ErrNotFound) {
			return st.Pipelines.Create(ctx, p)
		}
		if err != nil {
			return err
		}
		p.Version = old.Version
		return st.Pipelines.Update(ctx, p)
	}
	d.CheckExecution = func(app *model.App, _ bool) error {
		if app.ID == "" || app.BuildPlan == nil || app.BuildPlan.Kind != model.BuildPlanDockerfile || app.DeployTarget.Kind != model.DeployTargetDockerHost || app.DeployTarget.Prefix != prefix {
			return errors.New("unowned App execution target")
		}
		return nil
	}
	d.SourceCheckout = source.Checkout
	var imagePreparations atomic.Int32
	queue := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, RegistryAuthority: authority, RequireRegistryPins: true, RequireSecretPins: true, RequireImagePins: true, AdmissionGuard: storepg.NewTriggerStore(db), Deadline: 2 * time.Minute, ResolveSource: source.Resolve}
	queue.PrepareImages = func(ctx context.Context, plan *AppDeployPlan) (*AppImageResolution, error) {
		imagePreparations.Add(1)
		return d.PrepareImages(ctx, plan, RegistryImageResolver(nil))
	}
	intentID := "owned-manual-app-" + nonce
	receipt, err := queue.EnqueueIntent(ctx, app, "", &model.PipelineRun{StartedByUserSub: "owned-fixture-admin", StartedByEmail: "fixture@example.invalid"}, intentID)
	if err != nil {
		t.Fatal("App admission", err)
	}
	job, err := jobs.Get(ctx, receipt.JobID)
	if err != nil {
		t.Fatal(err)
	}
	var plan AppDeployPlan
	if json.Unmarshal(job.Payload, &plan) != nil || plan.App == nil || plan.Registry == nil || plan.Secrets == nil || plan.Images == nil || plan.Commit != commit || receipt.Commit != commit || plan.Registry.Subject != (model.RegistrySubject{Kind: "app", ID: app.ID}) || plan.Registry.Connections[config.ID].Credential.Mode != "database" || plan.Secrets.Mode != "database" || plan.Images.Kind != model.BuildPlanDockerfile || len(plan.Images.Services) != 0 || plan.Actor == nil || plan.Actor.StartedByUserSub != "owned-fixture-admin" {
		t.Fatal("immutable App source, secret, image or App-only registry evidence missing")
	}
	if plan.Secrets.EnvironmentID != appEnvironmentID || plan.Registry.Connections[config.ID].Credential.EnvironmentID != registryEnvironmentID {
		t.Fatal("App log environment and private registry scope were mixed")
	}
	// Advance main after durable acceptance. The worker must use the queued SHA.
	if err := os.WriteFile(filepath.Join(repository, "message.txt"), []byte("must-not-run-later-app-commit"), 0700); err != nil {
		t.Fatal(err)
	}
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "add", "message.txt")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "-c", "user.name=Owned fixture", "-c", "user.email=fixture@invalid.local", "commit", "-m", "advance App branch after admission")
	registryDispatch := jobqueue.NewRegistry()
	registryDispatch.Register(JobKindAppDeploy, queue.Handle)
	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{Store: jobs, Registry: registryDispatch, Size: 1, PollEvery: 50 * time.Millisecond, WorkerIDPrefix: prefix, LeaseTimeout: 30 * time.Second, HeartbeatEvery: 2 * time.Second, OnHeartbeat: HeartbeatJobRun(st), OnRecovery: RecoverJobRun(st), ClaimContext: func(ctx context.Context, job *jobqueue.Job) context.Context {
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
		job, err = jobs.Get(ctx, receipt.JobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == jobqueue.StatusSucceeded {
			break
		}
		if job.Status == jobqueue.StatusFailed || job.Status == jobqueue.StatusNeedsAttention {
			t.Fatal("genuine App worker did not complete successfully")
		}
		select {
		case <-ctx.Done():
			t.Fatal("App runtime deadline")
		case <-time.After(100 * time.Millisecond):
		}
	}
	cancelWorker()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	workerStopped = true
	run, err := st.Runs.Get(ctx, receipt.RunID)
	if err != nil || run.Status != model.RunStatusSuccess || run.PipelineID != receipt.PipelineID || run.Source != nil || run.StartedByUserSub != "owned-fixture-admin" {
		t.Fatal("App runtime run identity/status missing", err)
	}
	if imagePreparations.Load() != 1 || build.calls.Load() != 1 || push.calls.Load() != 1 || deploy.calls.Load() != 1 || authenticated.Load() == 0 || uploads.Load() == 0 || manifestReads.Load() == 0 {
		t.Fatal("actual App image/build/push/deploy did not execute once")
	}
	var buildDigest, pushDigest string
	var persistedLogText strings.Builder
	freshLogs := logstore.NewPostgres(db, logstore.Limits{})
	for _, stage := range run.StageRuns {
		if stage.Status != model.RunStatusSuccess {
			t.Fatal("App stage did not succeed")
		}
		entries, err := freshLogs.Read(ctx, run.ID, stage.StageID, 0)
		if err != nil || len(entries) == 0 {
			t.Fatalf("fresh PostgreSQL stage log missing: stage=%s err=%v", stage.StageID, err)
		}
		var stageText strings.Builder
		for _, entry := range entries {
			stageText.WriteString(entry.Line + "\n")
		}
		persistedLogText.WriteString(stageText.String())
		if stage.StageID == "build" {
			buildDigest = stage.Outputs["digest"]
			if !strings.Contains(stageText.String(), "built "+marker) {
				t.Fatal("real App BuildKit build-proof stdout missing from durable logs")
			}
		}
		if stage.StageID == "push" {
			pushDigest = stage.Outputs["digest"]
		}
	}
	if !strings.HasPrefix(pushDigest, "sha256:") || pushDigest != buildDigest || !strings.HasSuffix(deploy.image, "@"+pushDigest) {
		t.Fatal("App OCI/push/runtime digest mismatch")
	}
	history, err := st.AppDeploys.ListByApp(ctx, app.ID, 1)
	if err != nil || len(history) != 1 || history[0].RunID != run.ID || history[0].PipelineID != run.PipelineID || history[0].Status != model.RunStatusSuccess || history[0].ImageRef != deploy.image || history[0].Digest != pushDigest {
		t.Fatal("immutable App deployment history missing", err)
	}
	graph, err := st.Pipelines.Get(ctx, receipt.PipelineID)
	if err != nil || len(graph.Stages) != 3 {
		t.Fatal("recorded App graph missing", err)
	}
	for {
		state := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.State.Health.Status}}`, workload)
		if state == "healthy" {
			break
		}
		if state == "unhealthy" {
			t.Fatal("owned App workload is unhealthy")
		}
		select {
		case <-ctx.Done():
			t.Fatal("App readiness deadline")
		case <-time.After(200 * time.Millisecond):
		}
	}
	if nativeRuntimeCommand(ctx, t, "docker", "exec", workload, "/server", "probe") != marker {
		t.Fatal("App runtime consumed moved main instead of accepted SHA")
	}
	rawLogs := nativeRuntimeCommand(ctx, t, "docker", "logs", workload)
	if !strings.Contains(rawLogs, "ready "+marker) || !strings.Contains(rawLogs, "app-log "+appLogCanary) {
		t.Fatal("actual App stdout/canary was not emitted")
	}
	containerID := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, workload)
	logOpens := filepath.Join(base, "log-opens")
	dockerGuard := filepath.Join(base, "observe-docker")
	guard := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = inspect ] && [ \"$2\" = -f ] && [ \"$4\" = %s ] && [ \"$#\" = 4 ]; then\ncase \"$3\" in '{{.State.Status}}|'*|'{{.Id}}|'*) exec /usr/bin/docker \"$@\";; esac\nfi\nif [ \"$1\" = logs ] && [ \"$2\" = --follow ] && [ \"$3\" = --tail ] && [ \"$4\" = 200 ] && [ \"$5\" = %s ] && [ \"$#\" = 5 ]; then\nprintf 'opened\\n' >> %s\nexec /usr/bin/docker \"$@\"\nfi\nexit 97\n", workload, containerID, logOpens)
	if err := os.WriteFile(dockerGuard, []byte(guard), 0700); err != nil {
		t.Fatal(err)
	}
	runtime := &RuntimeService{Store: st, SecretGenerationBound: true, DockerBin: dockerGuard}
	// Mutable App edits must not redirect inspection from the recorded graph.
	app.DeployTarget = model.DeployTarget{Kind: model.DeployTargetKubernetes, Prefix: "unowned-current-target", Namespace: "unowned"}
	if err := st.Apps.Update(ctx, app); err != nil {
		t.Fatal("owned current-App edit failed", err)
	}
	app, err = st.Apps.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	status, err := runtime.Status(ctx, app, "deploy")
	if err != nil || status.Ref != workload || !status.Healthy || status.Image != deploy.image {
		t.Fatal("recorded App service status did not resolve owned runtime", err)
	}
	health, _, _ := runtime.Probe(ctx, app)
	if health != model.AppHealthHealthy {
		t.Fatal("real App runtime probe did not report healthy")
	}
	streamCtx, stopStream := context.WithTimeout(ctx, 1500*time.Millisecond)
	var redactedLogs bytes.Buffer
	streamErr := runtime.StreamDeploymentLogs(streamCtx, app, "deploy", jobs, d.EnvResolver, &redactedLogs)
	stopStream()
	if streamErr == nil || streamCtx.Err() != context.DeadlineExceeded || !strings.Contains(redactedLogs.String(), marker) || !strings.Contains(redactedLogs.String(), "app-log [REDACTED]") || strings.Contains(redactedLogs.String(), appLogCanary) || strings.Contains(redactedLogs.String(), password) {
		t.Fatal("generation-bound App logs were not safely observed through deliberate cancellation")
	}
	opened, err := os.ReadFile(logOpens)
	if err != nil || string(opened) != "opened\n" {
		t.Fatal("positive App log stream did not open exact container once")
	}
	if err := manager.Put(ctx, appEnvironmentID, "APP_LOG_CANARY", []byte("rotated-app-log-"+uuid.NewString())); err != nil {
		t.Fatal(err)
	}
	var deniedLogs bytes.Buffer
	if err := runtime.StreamDeploymentLogs(ctx, app, "deploy", jobs, d.EnvResolver, &deniedLogs); err == nil || deniedLogs.Len() != 0 {
		t.Fatal("historical App logs did not fail closed after secret rotation")
	}
	openedAfter, err := os.ReadFile(logOpens)
	if err != nil || string(openedAfter) != string(opened) {
		t.Fatal("rotated App generation opened historical raw log stream")
	}
	for _, root := range source.checkouts {
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("App source checkout retained")
		}
	}
	for _, path := range build.artifacts {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("private App OCI artifact retained after execution")
		}
	}
	if paths, err := filepath.Glob(filepath.Join(os.TempDir(), "cooker-registry-*")); err != nil || len(paths) != 0 {
		t.Fatal("private App temporary registry configuration retained")
	}
	encodedRun, _ := json.Marshal(run)
	encodedGraph, _ := json.Marshal(graph)
	encodedHistory, _ := json.Marshal(history)
	var storedRun, storedGraph string
	if err := db.QueryRowContext(ctx, `SELECT row_to_json(r)::text FROM pipeline_runs r WHERE id=$1`, run.ID).Scan(&storedRun); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT row_to_json(p)::text FROM pipelines p WHERE id=$1`, graph.ID).Scan(&storedGraph); err != nil {
		t.Fatal(err)
	}
	safeSources := map[string]string{"job": string(job.Payload), "run": string(encodedRun), "graph": string(encodedGraph), "history": string(encodedHistory), "storedRun": storedRun, "storedGraph": storedGraph, "durableLogs": persistedLogText.String(), "redactedRuntimeLogs": redactedLogs.String()}
	canaries := map[string]string{"registryValue": password, "registryBasicForm": username + ":" + password, "registryEncodedForm": base64.StdEncoding.EncodeToString([]byte(username + ":" + password)), "appLogValue": appLogCanary, "temporaryCheckoutPath": base, "sourceConnectionValue": "synthetic-app-fixture-connection-value"}
	for i, path := range build.artifacts {
		canaries[fmt.Sprint("temporaryOCIArtifactPath", i)] = path
	}
	violations := map[string][]string{}
	for category, canary := range canaries {
		for source, value := range safeSources {
			if strings.Contains(value, canary) {
				violations[category] = append(violations[category], source)
			}
		}
	}
	if len(violations) != 0 {
		// Persist categories only: diagnostics must not repeat the leaked values.
		data, _ := json.MarshalIndent(map[string]any{"scope": "owned-local-App-safe-persistence-negative", "violations": violations, "commit": commit, "runId": run.ID, "ociDigest": pushDigest, "runtimeObservationReached": true, "generationBoundRedactionReached": true, "rotationDenialReached": true}, "", "  ")
		if err := os.WriteFile(os.Getenv("COOKER_APP_RUNTIME_EVIDENCE")+".negative.json", append(data, '\n'), 0600); err != nil {
			t.Error(err)
		}
		t.Fatal("private App value or checkout path entered safe persisted/runtime evidence")
	}
	// Reopened manual receipt wins despite changed grants/environment/config.
	if _, err := st.Registries.(interface {
		UpdateGrants(context.Context, string, int64, []string, []string) (*model.RegistryConfig, error)
	}).UpdateGrants(ctx, config.ID, config.Version, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := source.calls.Load()
	reopened := &AppDeployQueue{Jobs: jobqueue.NewPostgres(db), RequireRegistryPins: true, RequireSecretPins: true, RequireImagePins: true}
	for i := 0; i < 3; i++ {
		retry, err := reopened.EnqueueIntent(ctx, app, commit, nil, intentID)
		if err != nil || *retry != *receipt {
			t.Fatal("App receipt-first reopen lost accepted identity", err)
		}
	}
	if source.calls.Load() != before || imagePreparations.Load() != 1 || build.calls.Load() != 1 || push.calls.Load() != 1 || deploy.calls.Load() != 1 {
		t.Fatal("App receipt replay repeated preparation or runtime effects")
	}
	evidence := map[string]any{"scope": "owned-local-manual-app-runtime-v1", "commit": commit, "jobId": job.ID, "jobStatus": job.Status, "receipt": receipt, "runId": run.ID, "appId": app.ID, "registrySubject": plan.Registry.Subject, "appEnvironmentId": appEnvironmentID, "registryEnvironmentId": registryEnvironmentID, "secretPlanMode": plan.Secrets.Mode, "ociDigest": pushDigest, "sourceOperations": before, "imagePreparations": imagePreparations.Load(), "builds": build.calls.Load(), "pushes": push.calls.Load(), "deploys": deploy.calls.Load(), "authenticatedRequests": authenticated.Load(), "manifestUploads": uploads.Load(), "manifestReads": manifestReads.Load(), "runtimeStatus": status, "health": health, "serviceKey": "deploy", "generationBoundLogsRedacted": true, "historicalLogsDeniedAfterRotationBeforeOpen": true, "persistedCurrentAppEditsCannotRedirectObservation": true, "temporaryOCIAndRegistryConfigurationCleaned": true, "receiptRetries": 3, "runtimeContainerId": containerID, "runtimeImageId": nativeRuntimeCommand(ctx, t, "docker", "image", "inspect", "--format", `{{.Id}}`, deploy.image), "runtimeImageRef": deploy.image}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("COOKER_APP_RUNTIME_EVIDENCE"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func appRuntimeOwnedDatabaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/cooker_app_runtime_test" || u.User == nil || u.User.Username() != "app_runtime_fixture" || u.Query().Get("sslmode") != "disable" || len(u.Query()) != 1 || u.Fragment != "" {
		return nil, errors.New("refusing non-owned App runtime database")
	}
	if _, password := u.User.Password(); password {
		return nil, errors.New("App runtime database must not read a credential")
	}
	return u, nil
}

func appRuntimeCleanup(t *testing.T, nonce, workload, repository, image string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	evidence := map[string]any{"scope": "owned-local-app-runtime-cleanup", "nonce": nonce, "containerName": workload}
	if nativeRuntimeExists(ctx, "container", workload) {
		id := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, workload)
		if nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{index .Config.Labels "cooker.fixture"}}`, id) != nonce {
			t.Error("refusing unowned App runtime cleanup")
			return
		}
		ref := nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Config.Image}}`, id)
		nativeRuntimeCommand(ctx, t, "docker", "rm", "-f", id)
		evidence["removedContainerId"], evidence["imageRef"] = id, ref
		if strings.HasPrefix(ref, repository+"@sha256:") && nativeRuntimeExists(ctx, "image", ref) {
			nativeRuntimeCommand(ctx, t, "docker", "image", "rm", ref)
		}
	}
	if strings.HasPrefix(image, repository+"@sha256:") && nativeRuntimeExists(ctx, "image", image) {
		nativeRuntimeCommand(ctx, t, "docker", "image", "rm", image)
	}
	evidence["remainingContainer"] = nativeRuntimeExists(ctx, "container", workload)
	evidence["remainingImage"] = image != "" && nativeRuntimeExists(ctx, "image", image)
	if path := os.Getenv("COOKER_APP_RUNTIME_EVIDENCE"); path != "" {
		data, _ := json.MarshalIndent(evidence, "", "  ")
		if err := os.WriteFile(path+".cleanup.json", append(data, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}
}

type appRuntimeGit struct {
	root, base, connectionID, repositoryID, acceptedCommit string
	connections                                            connection.Store
	version                                                int
	calls                                                  atomic.Int32
	checkouts                                              []string
}

func (s *appRuntimeGit) check(ctx context.Context, app *model.App) error {
	if app == nil || app.GitHubRepo != "owned/app-runtime" || app.BuildPlan == nil || app.BuildPlan.SourceConnectionID != s.connectionID || app.BuildPlan.SourceConnectionVersion != s.version || app.BuildPlan.SourceRepositoryID != s.repositoryID {
		return errors.New("unowned App source identity")
	}
	c, err := s.connections.Get(ctx, s.connectionID)
	if err != nil || !c.Enabled || c.Version != s.version || c.Provider != "github" || !c.Allows(app.GitHubRepo) {
		return errors.New("owned App source connection changed")
	}
	return nil
}

func (s *appRuntimeGit) Resolve(ctx context.Context, app *model.App, ref string) (string, error) {
	s.calls.Add(1)
	if err := s.check(ctx, app); err != nil {
		return "", err
	}
	if ref != "main" {
		return "", errors.New("unowned App ref")
	}
	out, err := exec.CommandContext(ctx, "git", "-C", s.root, "rev-parse", "--verify", "refs/heads/main^{commit}").Output()
	commit := strings.TrimSpace(string(out))
	if err != nil || commit != s.acceptedCommit {
		return "", errors.New("owned App ref did not resolve accepted commit")
	}
	return commit, nil
}

func (s *appRuntimeGit) Checkout(ctx context.Context, app *model.App, commit string, _ io.Writer) (string, error) {
	s.calls.Add(1)
	if err := s.check(ctx, app); err != nil {
		return "", err
	}
	if commit != s.acceptedCommit {
		return "", errors.New("App checkout did not consume immutable queued commit")
	}
	root, err := os.MkdirTemp(s.base, "checkout-")
	if err != nil {
		return "", err
	}
	s.checkouts = append(s.checkouts, root)
	for _, args := range [][]string{{"clone", "--no-hardlinks", "--no-checkout", "--", s.root, root}, {"-C", root, "checkout", "--detach", commit}} {
		if err := exec.CommandContext(ctx, "git", args...).Run(); err != nil {
			os.RemoveAll(root)
			return "", errors.New("owned App Git checkout failed")
		}
	}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) != commit {
		os.RemoveAll(root)
		return "", errors.New("owned App checkout HEAD differs from queued commit")
	}
	return root, nil
}

type appRuntimeBuild struct {
	delegate       builder.Builder
	marker, commit string
	calls          atomic.Int32
	artifacts      []string
}

func (b *appRuntimeBuild) Build(ctx context.Context, req builder.Request) (builder.Result, error) {
	b.calls.Add(1)
	marker, err := os.ReadFile(filepath.Join(req.ContextDir, "message.txt"))
	if err != nil || string(marker) != b.marker || len(req.BuildArgs) != 0 || req.RegistryAuth == nil {
		return builder.Result{}, errors.New("App build did not consume accepted source with scoped authority")
	}
	out, err := exec.CommandContext(ctx, "git", "-C", req.ContextDir, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) != b.commit {
		return builder.Result{}, errors.New("App builder received another source checkout")
	}
	ignore, err := os.ReadFile(filepath.Join(req.ContextDir, ".dockerignore"))
	if err != nil || string(ignore) != ".git\n" {
		return builder.Result{}, errors.New("App source must exclude private Git metadata from build input")
	}
	result, err := b.delegate.Build(ctx, req)
	if result.Artifact != nil {
		b.artifacts = append(b.artifacts, result.Artifact.OutputDir())
	}
	return result, err
}

type appRuntimeDeploy struct {
	delegate                                           deployer.Deployer
	name, nonce, network, image, repository, appCanary string
	calls                                              atomic.Int32
	registryReads                                      *atomic.Int32
}

func (d *appRuntimeDeploy) Deploy(ctx context.Context, req deployer.Request) (deployer.Result, error) {
	d.calls.Add(1)
	if req.Kind != deployer.KindDockerRun || req.Name != d.name || req.RegistryAuth == nil || !strings.HasPrefix(req.Image, d.repository+"@sha256:") || len(req.Env) != 1 || req.Env["APP_LOG_CANARY"] != d.appCanary || len(req.Ports) != 0 || nativeRuntimeExists(ctx, "container", req.Name) || nativeRuntimeExists(ctx, "image", req.Image) {
		return deployer.Result{}, errors.New("refusing unowned or preloaded App runtime fixture")
	}
	d.image = req.Image
	if req.Labels == nil {
		req.Labels = map[string]string{}
	}
	if req.Labels[RuntimeRunLabel] == "" {
		return deployer.Result{}, errors.New("App runtime has no accepted run label")
	}
	req.Labels["cooker.fixture"], req.Labels["cooker.agent"], req.Network = d.nonce, "catalog_review", d.network
	req.Resources = &deployer.ResourceLimits{Memory: "128m", CPUs: "0.5"}
	before := d.registryReads.Load()
	result, err := d.delegate.Deploy(ctx, req)
	if err == nil && d.registryReads.Load() == before {
		return result, errors.New("App runtime did not pull through authenticated registry")
	}
	return result, err
}
