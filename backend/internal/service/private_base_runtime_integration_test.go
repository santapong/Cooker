//go:build integration

package service

import (
	"archive/tar"
	"bytes"
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
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
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

// These isolated opt-in flows preserve the native/App acceptance helpers. Only
// the base-image input and trusted observing delegates differ; actual BuildKit
// session authentication, immutable authority and runtime transports execute.
func TestNativePrivateBaseGenuineLocalRuntime(t *testing.T) {
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

	fixture := privateBaseFixture(t, ctx, nonce)
	registryServer := fixture
	username, password := fixture.Username, fixture.Password
	var authenticated, uploads, manifestReads atomic.Int32
	registryHost := fixture.Host
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
		"Dockerfile":  []byte("FROM " + fixture.BaseRef + "\nCOPY server /server\nCOPY message.txt /message.txt\nRUN [\"/server\",\"build-proof\"]\nHEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=15 CMD [\"/server\",\"probe\"]\nENTRYPOINT [\"/server\"]\n"),
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
	build := &nativeRuntimeBuild{delegate: &privateBaseBuilder{Delegate: builder.NewBuildKit(buildkitURL.String()), Fixture: fixture, Subject: model.RegistrySubject{Kind: "pipeline", ID: p.ID}, ConnectionID: config.ID}, marker: marker}
	push := &nativeRuntimePush{delegate: &privateBasePusher{Delegate: pusher.NewCrane(), Fixture: fixture, Authenticated: &authenticated, Uploads: &uploads, Reads: &manifestReads}}
	deploy := &nativeRuntimeDeploy{delegate: &privateBaseDeployer{Delegate: deployer.NewDockerRun(), Fixture: fixture, Authenticated: &authenticated, Uploads: &uploads, Reads: &manifestReads}, name: workload, nonce: nonce, network: network, registryReads: &manifestReads}
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
	if nativeRuntimeCommand(ctx, t, "docker", "exec", workload, "/server", "base-proof") != fixture.Marker {
		t.Fatal("runtime did not retain private base identity")
	}
	fixture.Mu.Lock()
	fixture.RuntimeVerified = true
	fixture.Mu.Unlock()
	fixture.AssertPrivatePull(t)
	evidence := map[string]any{"privateBase": fixture.Proof(), "scope": "owned-local-native-runtime-v1", "commit": commit, "source": run.Source, "jobId": job.ID, "jobStatus": job.Status, "receipt": accepted.Receipt, "runId": run.ID, "ociDigest": pushDigest, "sourceOperations": before, "registryPreparations": pins.Load(), "builds": build.calls.Load(), "pushes": push.calls.Load(), "deploys": deploy.calls.Load(), "authenticatedRequests": authenticated.Load(), "manifestUploads": uploads.Load(), "manifestReads": manifestReads.Load(), "dockerHealth": "healthy", "containerStdoutVerified": true, "receiptRetries": 3, "runtimeContainerId": nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.Id}}`, workload), "runtimeImageId": nativeRuntimeCommand(ctx, t, "docker", "image", "inspect", "--format", `{{.Id}}`, deploy.image), "runtimeImageRef": deploy.image}
	if path := os.Getenv("COOKER_NATIVE_RUNTIME_EVIDENCE"); path != "" {
		data, _ := json.MarshalIndent(evidence, "", "  ")
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("real local Git checkout -> private-FROM BuildKit OCI -> authenticated registry -> Docker pull/health/log proven; hosted metadata and native observation API remain unclaimed")
}

func TestAppPrivateBaseGenuineLocalRuntime(t *testing.T) {
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

	fixture := privateBaseFixture(t, ctx, nonce)
	registryServer := fixture
	username, password := fixture.Username, fixture.Password
	var authenticated, uploads, manifestReads atomic.Int32
	registryHost := fixture.Host
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
		"Dockerfile":    []byte("FROM " + fixture.BaseRef + "\nCOPY server /server\nCOPY message.txt /message.txt\nRUN [\"/server\",\"build-proof\"]\nHEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=15 CMD [\"/server\",\"probe\"]\nENTRYPOINT [\"/server\"]\n"),
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
	build := &appRuntimeBuild{delegate: &privateBaseBuilder{Delegate: builder.NewBuildKit(buildkitURL.String()), Fixture: fixture, Subject: model.RegistrySubject{Kind: "app", ID: app.ID}, ConnectionID: config.ID}, marker: marker, commit: commit}
	push := &nativeRuntimePush{delegate: &privateBasePusher{Delegate: pusher.NewCrane(), Fixture: fixture, Authenticated: &authenticated, Uploads: &uploads, Reads: &manifestReads}}
	deploy := &appRuntimeDeploy{delegate: &privateBaseDeployer{Delegate: deployer.NewDockerRun(), Fixture: fixture, Authenticated: &authenticated, Uploads: &uploads, Reads: &manifestReads}, name: workload, nonce: nonce, network: network, appCanary: appLogCanary, repository: repositoryPrefix, registryReads: &manifestReads}
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
		data, _ := json.MarshalIndent(map[string]any{"privateBase": fixture.Proof(), "scope": "owned-local-App-safe-persistence-negative", "violations": violations, "commit": commit, "runId": run.ID, "ociDigest": pushDigest, "runtimeObservationReached": true, "generationBoundRedactionReached": true, "rotationDenialReached": true}, "", "  ")
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
	if nativeRuntimeCommand(ctx, t, "docker", "exec", workload, "/server", "base-proof") != fixture.Marker {
		t.Fatal("runtime did not retain private base identity")
	}
	fixture.Mu.Lock()
	fixture.RuntimeVerified = true
	fixture.Mu.Unlock()
	fixture.AssertPrivatePull(t)
	evidence := map[string]any{"privateBase": fixture.Proof(), "scope": "owned-local-manual-app-runtime-v1", "commit": commit, "jobId": job.ID, "jobStatus": job.Status, "receipt": receipt, "runId": run.ID, "appId": app.ID, "registrySubject": plan.Registry.Subject, "appEnvironmentId": appEnvironmentID, "registryEnvironmentId": registryEnvironmentID, "secretPlanMode": plan.Secrets.Mode, "ociDigest": pushDigest, "sourceOperations": before, "imagePreparations": imagePreparations.Load(), "builds": build.calls.Load(), "pushes": push.calls.Load(), "deploys": deploy.calls.Load(), "authenticatedRequests": authenticated.Load(), "manifestUploads": uploads.Load(), "manifestReads": manifestReads.Load(), "runtimeStatus": status, "health": health, "serviceKey": "deploy", "generationBoundLogsRedacted": true, "historicalLogsDeniedAfterRotationBeforeOpen": true, "persistedCurrentAppEditsCannotRedirectObservation": true, "temporaryOCIAndRegistryConfigurationCleaned": true, "receiptRetries": 3, "runtimeContainerId": containerID, "runtimeImageId": nativeRuntimeCommand(ctx, t, "docker", "image", "inspect", "--format", `{{.Id}}`, deploy.image), "runtimeImageRef": deploy.image}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("COOKER_APP_RUNTIME_EVIDENCE"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

// Registry counters exclude setup relays from daemon-local private-base reads.
type privateBaseCounts struct {
	Authenticated, Challenges, ManifestUploads, ManifestReads int32
	DaemonBaseManifestReads, DaemonBaseBlobReads              int32
}
type privateBaseRegistry struct {
	URL, Host, BaseRef, Marker, Username, Password, Nonce string
	LayerDigest, LayerDiffID                              string
	Ctx                                                   context.Context
	Client                                                *http.Client
	Command                                               *exec.Cmd
	Mu                                                    sync.Mutex
	Setup, AfterBuild, AfterPush, AfterPull               privateBaseCounts
	SessionHosts                                          []string
	SessionCalls                                          int
	Subject                                               model.RegistrySubject
	ConnectionID                                          string
	LayerVerified, RuntimeVerified                        bool
}

func privateBaseFixture(t *testing.T, ctx context.Context, nonce string) *privateBaseRegistry {
	t.Helper()
	if os.Getenv("COOKER_PRIVATE_BASE_ACCEPTANCE") != "owned-local-v1" {
		t.Fatal("explicit owned private-base harness required")
	}
	id := os.Getenv("COOKER_PRIVATE_BASE_REGISTRY_ID")
	if len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{index .Config.Labels "cooker.fixture"}}`, id) != nonce || nativeRuntimeCommand(ctx, t, "docker", "inspect", "--format", `{{.HostConfig.Privileged}}`, id) != "false" {
		t.Fatal("unowned private registry namespace")
	}
	port := os.Getenv("COOKER_PRIVATE_BASE_REGISTRY_PORT")
	var p int
	if _, err := fmt.Sscan(port, &p); err != nil || fmt.Sprint(p) != port || p < 1024 || p > 65535 {
		t.Fatal("invalid owned registry port")
	}
	f := &privateBaseRegistry{Host: "127.0.0.1:" + port, Marker: "owned-base-" + nonce, Username: "owned-private-base", Password: "owned-private-base-" + uuid.NewString(), Nonce: nonce, Ctx: ctx, Client: &http.Client{Timeout: 3 * time.Second}}
	f.URL = "http://" + f.Host
	configuration, _ := json.Marshal(map[string]any{"Nonce": nonce, "Username": f.Username, "Password": f.Password, "Port": p})
	f.Command = exec.CommandContext(ctx, "docker", "exec", "-i", id, "/private-registry")
	f.Command.Stdin = bytes.NewReader(configuration)
	f.Command.Stdout, f.Command.Stderr = io.Discard, io.Discard
	if f.Command.Start() != nil {
		t.Fatal("owned registry process unavailable")
	}
	t.Cleanup(func() { f.Close() })
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := f.Counts(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned registry namespace readiness failed; no auth verdict")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var contents bytes.Buffer
	tarWriter := tar.NewWriter(&contents)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "base-identity", Mode: 0444, Size: int64(len(f.Marker))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte(f.Marker)); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	layer, err := tarball.LayerFromReader(bytes.NewReader(contents.Bytes()))
	if err != nil {
		t.Fatal("owned base layer unavailable")
	}
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatal("owned base image unavailable")
	}
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	config.OS, config.Architecture = "linux", "amd64"
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := layer.Digest()
	if err != nil {
		t.Fatal(err)
	}
	diff, err := layer.DiffID()
	if err != nil {
		t.Fatal(err)
	}
	f.LayerDigest, f.LayerDiffID = compressed.String(), diff.String()
	ref, err := name.NewTag(f.Host+"/owned/base-"+nonce+":setup", name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if remote.Write(ref, image, remote.WithContext(ctx), remote.WithAuth(authn.FromConfig(authn.AuthConfig{Username: f.Username, Password: f.Password}))) != nil {
		t.Fatal("owned private base setup failed; no auth verdict")
	}
	f.BaseRef = f.Host + "/owned/base-" + nonce + "@" + digest.String()
	f.Setup, err = f.Counts()
	if err != nil || f.Setup.ManifestUploads != 1 || f.Setup.DaemonBaseManifestReads != 0 || f.Setup.DaemonBaseBlobReads != 0 {
		t.Fatal("base setup/daemon separation failed")
	}
	return f
}
func (f *privateBaseRegistry) Close() {
	if f.Command == nil {
		return
	}
	request, _ := http.NewRequest(http.MethodPost, f.URL+"/_owned/stop/"+f.Nonce, nil)
	if response, err := f.Client.Do(request); err == nil {
		response.Body.Close()
	}
	f.Command.Wait()
	f.Command = nil
}
func (f *privateBaseRegistry) Counts() (privateBaseCounts, error) {
	var c privateBaseCounts
	response, err := f.Client.Get(f.URL + "/_owned/stats/" + f.Nonce)
	if err != nil {
		return c, errors.New("owned registry counters unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&c) != nil {
		return c, errors.New("owned registry counter shape unavailable")
	}
	return c, nil
}
func (f *privateBaseRegistry) AssertPrivatePull(t *testing.T) {
	t.Helper()
	id := os.Getenv("COOKER_PRIVATE_BASE_BUILDKIT_ID")
	if len(id) != 64 || nativeRuntimeCommand(f.Ctx, t, "docker", "inspect", "--format", `{{index .Config.Labels "cooker.fixture"}}`, id) != f.Nonce {
		t.Fatal("unowned daemon log scope")
	}
	raw, err := exec.CommandContext(f.Ctx, "docker", "logs", id).CombinedOutput()
	if err != nil {
		t.Fatal("owned daemon logs unavailable")
	}
	for _, canary := range []string{f.Password, f.Username + ":" + f.Password, base64.StdEncoding.EncodeToString([]byte(f.Username + ":" + f.Password))} {
		if strings.Contains(string(raw), canary) {
			t.Fatal("private credential appeared in owned daemon logs")
		}
	}

	f.Mu.Lock()
	defer f.Mu.Unlock()
	if !f.LayerVerified || !f.RuntimeVerified || f.SessionCalls < 1 || len(f.SessionHosts) != 1 || f.SessionHosts[0] != f.Host || f.AfterBuild.DaemonBaseManifestReads < 1 || f.AfterBuild.DaemonBaseBlobReads < 1 || f.Subject.Validate() != nil || f.ConnectionID == "" {
		t.Fatal("actual subject-bound BuildKit session/private base evidence missing")
	}
}
func (f *privateBaseRegistry) Proof() map[string]any {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	return map[string]any{"baseRef": f.BaseRef, "baseLayerDigest": f.LayerDigest, "baseLayerDiffId": f.LayerDiffID, "baseMarker": f.Marker, "baseLayerVerified": f.LayerVerified, "setup": f.Setup, "afterBuild": f.AfterBuild, "afterPush": f.AfterPush, "afterPull": f.AfterPull, "sessionCalls": f.SessionCalls, "sessionHosts": f.SessionHosts, "registrySubject": f.Subject, "registryConnectionId": f.ConnectionID, "runtimeBaseIdentityVerified": f.RuntimeVerified}
}

type privateBaseBuilder struct {
	Delegate     builder.Builder
	Fixture      *privateBaseRegistry
	Subject      model.RegistrySubject
	ConnectionID string
}

func (b *privateBaseBuilder) Build(ctx context.Context, req builder.Request) (builder.Result, error) {
	accepted, ok := ctx.Value(acceptedRegistryAuthorityKey{}).(acceptedRegistryAuthority)
	if !ok || accepted.Invalid || accepted.Subject != b.Subject || accepted.Plan == nil || accepted.Plan.Connections[b.ConnectionID].Host != b.Fixture.Host || req.RegistryAuth == nil {
		return builder.Result{}, errors.New("private base did not receive accepted subject-bound authority")
	}
	delegate := req.RegistryAuth
	req.RegistryAuth = func(callCtx context.Context, host string) (authn.Authenticator, error) {
		b.Fixture.Mu.Lock()
		b.Fixture.SessionCalls++
		if !slicesContains(b.Fixture.SessionHosts, host) {
			b.Fixture.SessionHosts = append(b.Fixture.SessionHosts, host)
		}
		b.Fixture.Subject, b.Fixture.ConnectionID = b.Subject, b.ConnectionID
		b.Fixture.Mu.Unlock()
		return delegate(callCtx, host)
	}
	result, err := b.Delegate.Build(ctx, req)
	if err != nil {
		return result, err
	}
	retained := false
	defer func() {
		if !retained && result.Artifact != nil {
			result.Artifact.Close()
		}
	}()
	store, descriptor, err := result.Artifact.Contents()
	if err != nil {
		return builder.Result{}, errors.New("private base OCI artifact unavailable")
	}
	var image v1.Image
	if descriptor.MediaType.IsIndex() {
		rootIndex, e := store.ImageIndex()
		if e != nil {
			return builder.Result{}, errors.New("private base output root index unavailable")
		}
		index, e := rootIndex.ImageIndex(descriptor.Digest)
		if e != nil {
			return builder.Result{}, errors.New("private base output index unavailable")
		}
		manifest, e := index.IndexManifest()
		if e != nil || len(manifest.Manifests) != 1 {
			return builder.Result{}, errors.New("private base output platform ambiguous")
		}
		image, err = index.Image(manifest.Manifests[0].Digest)
	} else {
		image, err = store.Image(descriptor.Digest)
	}
	if err != nil {
		return builder.Result{}, errors.New("private base output image unavailable")
	}
	config, err := image.ConfigFile()
	if err != nil || len(config.RootFS.DiffIDs) < 1 || config.RootFS.DiffIDs[0].String() != b.Fixture.LayerDiffID {
		return builder.Result{}, errors.New("private base layer changed in output")
	}
	layers, err := image.Layers()
	if err != nil || len(layers) < 1 {
		return builder.Result{}, errors.New("private base output layers missing")
	}
	reader, err := layers[0].Uncompressed()
	if err != nil {
		return builder.Result{}, errors.New("private base layer unreadable")
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	found := false
	for {
		header, e := tarReader.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return builder.Result{}, errors.New("private base layer malformed")
		}
		if strings.TrimPrefix(header.Name, "/") == "base-identity" {
			value, e := io.ReadAll(io.LimitReader(tarReader, 4096))
			if e != nil || string(value) != b.Fixture.Marker {
				return builder.Result{}, errors.New("private base identity differs")
			}
			found = true
		}
	}
	if !found {
		return builder.Result{}, errors.New("private base identity not retained")
	}
	counts, err := b.Fixture.Counts()
	if err != nil {
		return builder.Result{}, err
	}
	b.Fixture.Mu.Lock()
	b.Fixture.AfterBuild = counts
	b.Fixture.LayerVerified = true
	b.Fixture.Mu.Unlock()
	retained = true
	return result, nil
}
func slicesContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

type privateBasePusher struct {
	Delegate                      pusher.Pusher
	Fixture                       *privateBaseRegistry
	Authenticated, Uploads, Reads *atomic.Int32
}

func (p *privateBasePusher) Push(ctx context.Context, req pusher.Request) (pusher.Result, error) {
	result, err := p.Delegate.Push(ctx, req)
	if err != nil {
		return result, err
	}
	counts, err := p.Fixture.Counts()
	if err != nil {
		return result, err
	}
	p.Authenticated.Store(counts.Authenticated)
	p.Uploads.Store(counts.ManifestUploads)
	p.Reads.Store(counts.ManifestReads)
	p.Fixture.Mu.Lock()
	p.Fixture.AfterPush = counts
	p.Fixture.Mu.Unlock()
	return result, nil
}

type privateBaseDeployer struct {
	Delegate                      deployer.Deployer
	Fixture                       *privateBaseRegistry
	Authenticated, Uploads, Reads *atomic.Int32
}

func (d *privateBaseDeployer) Deploy(ctx context.Context, req deployer.Request) (deployer.Result, error) {
	result, err := d.Delegate.Deploy(ctx, req)
	if err != nil {
		return result, err
	}
	counts, err := d.Fixture.Counts()
	if err != nil {
		return result, err
	}
	d.Authenticated.Store(counts.Authenticated)
	d.Uploads.Store(counts.ManifestUploads)
	d.Reads.Store(counts.ManifestReads)
	d.Fixture.Mu.Lock()
	d.Fixture.AfterPull = counts
	d.Fixture.Mu.Unlock()
	return result, nil
}

func TestPrivateBaseAnonymousDoesNotBorrowUnrelatedConnection(t *testing.T) {
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

	fixture := privateBaseFixture(t, ctx, nonce)
	codecKey := make([]byte, 32)
	if _, err = rand.Read(codecKey); err != nil {
		t.Fatal(err)
	}
	codec, err := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(codecKey))
	if err != nil {
		t.Fatal(err)
	}
	incarnation := uuid.NewString()
	envID := "registry_" + strings.ReplaceAll(incarnation, "-", "")
	if err = st.Environments.Create(ctx, &model.Environment{ID: envID, Name: envID, PlainVars: map[string]string{}, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	manager := database.New(storepg.NewEnvironmentStore(db), codec)
	if err = manager.Put(ctx, envID, "password", []byte(fixture.Password)); err != nil {
		t.Fatal(err)
	}
	unrelated := &model.RegistryConfig{ID: uuid.NewString(), Name: "Unrelated valid private base connection", URL: fixture.URL, Username: fixture.Username, PasswordRef: "registry:v2:" + incarnation, AllowedPipelineIDs: []string{uuid.NewString()}}
	if err = st.Registries.Create(ctx, unrelated); err != nil {
		t.Fatal(err)
	}
	subject := model.RegistrySubject{Kind: "pipeline", ID: uuid.NewString()}
	authority := NewRegistryAuthorityService(st, manager)
	authority.Registries, authority.Environments = storepg.NewRegistryConfigStore(db), storepg.NewEnvironmentStore(db)
	plan, err := authority.Prepare(ctx, subject, nil, map[string][]string{"build": nil})
	if err != nil || len(plan.Connections) != 0 {
		t.Fatal("explicit anonymous scope could not be prepared")
	}
	resolver, err := authority.Resolver(ctx, subject, plan, "build")
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	observing := func(callCtx context.Context, host string) (authn.Authenticator, error) {
		calls++
		if host != fixture.Host {
			return nil, errors.New("unexpected private base host")
		}
		return resolver(callCtx, host)
	}
	root := t.TempDir()
	if os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM "+fixture.BaseRef+"\n"), 0600) != nil {
		t.Fatal("owned anonymous context unavailable")
	}
	endpoint := os.Getenv("COOKER_NATIVE_RUNTIME_BUILDKIT")
	var output bytes.Buffer
	result, buildErr := builder.NewBuildKit(endpoint).Build(ctx, builder.Request{ContextDir: root, RegistryAuth: observing, RegistryHosts: []string{}, LogWriter: &output})
	if buildErr == nil || result.Artifact != nil {
		if result.Artifact != nil {
			result.Artifact.Close()
		}
		t.Fatal("anonymous private FROM succeeded or retained OCI output")
	}
	after, err := fixture.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if calls < 1 || after.Challenges <= fixture.Setup.Challenges || after.Authenticated != fixture.Setup.Authenticated || after.DaemonBaseManifestReads != 0 || after.DaemonBaseBlobReads != 0 {
		t.Fatal("anonymous denial did not prove absence of unrelated same-host fallback")
	}
	for _, canary := range []string{fixture.Password, fixture.Username + ":" + fixture.Password, base64.StdEncoding.EncodeToString([]byte(fixture.Username + ":" + fixture.Password))} {
		if strings.Contains(output.String()+buildErr.Error(), canary) {
			t.Fatal("private registry value entered anonymous failure output")
		}
	}
	encoded, _ := json.Marshal(plan)
	if strings.Contains(string(encoded), fixture.Password) || len(plan.Connections) != 0 {
		t.Fatal("anonymous immutable evidence contains unrelated credentials")
	}
	evidence := map[string]any{"scope": "owned-private-base-anonymous-negative", "subject": subject, "unrelatedStoredConnectionId": unrelated.ID, "acceptedConnectionCount": len(plan.Connections), "baseRef": fixture.BaseRef, "setup": fixture.Setup, "afterAnonymous": after, "sessionCalls": calls, "noOCIArtifact": true, "noUnrelatedHostFallback": true, "safeError": "private base requires accepted registry authority", "secretCanariesAbsent": true}
	data, _ := json.MarshalIndent(evidence, "", "  ")
	if os.WriteFile(os.Getenv("COOKER_NATIVE_RUNTIME_EVIDENCE"), append(data, '\n'), 0600) != nil {
		t.Fatal("owned anonymous evidence unavailable")
	}
}
