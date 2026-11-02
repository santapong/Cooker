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
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	cookercrypto "github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
	"gopkg.in/yaml.v3"
)

// This opt-in test creates real Docker workloads using a local Git fixture and
// a dedicated PostgreSQL database. It does not use GitHub or existing services.
func TestAppQueueLiveCompose(t *testing.T) {
	dsn := os.Getenv("COOKER_APP_ACCEPTANCE_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated local acceptance database required")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || !strings.HasPrefix(u.Path, "/cooker_acceptance") {
		t.Fatal("acceptance requires a dedicated cooker_acceptance* database on loopback")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	st, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	redisImage := liveDockerImage(t, ctx, "redis:7-alpine")
	t.Run("redis", func(t *testing.T) {
		compose := fmt.Sprintf("services:\n  redis:\n    image: %s\n    healthcheck:\n      test: [CMD, redis-cli, ping]\n      interval: 1s\n      timeout: 1s\n      retries: 30\n", redisImage)
		liveQueueCase(t, ctx, st, db, dsn, "redis", compose, nil, nil, []string{"redis"}, false)
	})
	if os.Getenv("COOKER_APP_ACCEPTANCE_SCOPED_N8N") == "true" {
		t.Run("n8n_scoped", func(t *testing.T) {
			liveN8nQueueCase(t, ctx, st, db, dsn, redisImage, true)
		})
	}
	if os.Getenv("COOKER_APP_ACCEPTANCE_N8N") != "true" {
		return
	}
	t.Run("n8n", func(t *testing.T) {
		liveN8nQueueCase(t, ctx, st, db, dsn, redisImage, false)
	})
}

func liveN8nQueueCase(t *testing.T, ctx context.Context, st *store.Store, db *sql.DB, dsn, redisImage string, scoped bool) {
	t.Helper()
	data, err := os.ReadFile("testdata/n8n-queue.compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	plain := map[string]string{"N8N_IMAGE": liveDockerImage(t, ctx, "n8nio/n8n:latest"), "REDIS_IMAGE": redisImage, "POSTGRES_IMAGE": liveDockerImage(t, ctx, "postgres:16-alpine"), "N8N_PORT": fmt.Sprint(port)}
	secret := map[string]string{"POSTGRES_PASSWORD": liveRandom(t), "N8N_ENCRYPTION_KEY": liveRandom(t)}
	if scoped {
		// Explicit scope rejects globally interpolated extension templates.
		// Expand the fixture's aliases into service-local environment maps;
		// keep the original file untouched for the legacy acceptance case.
		var document map[string]any
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		delete(document, "x-n8n-env")
		data, err = yaml.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		secret["COOKER_UNBOUND_SECRET"] = liveRandom(t)
		scope := map[string][]string{
			"postgres":   {"POSTGRES_PASSWORD"},
			"redis":      {},
			"n8n-main":   {"POSTGRES_PASSWORD", "N8N_ENCRYPTION_KEY"},
			"n8n-worker": {"POSTGRES_PASSWORD", "N8N_ENCRYPTION_KEY"},
		}
		liveQueueCase(t, ctx, st, db, dsn, "n8n", string(data), plain, secret, []string{"postgres", "redis", "n8n-main", "n8n-worker"}, true, scope)
		return
	}
	liveQueueCase(t, ctx, st, db, dsn, "n8n", string(data), plain, secret, []string{"postgres", "redis", "n8n-main", "n8n-worker"}, true)
}

func liveQueueCase(t *testing.T, ctx context.Context, st *store.Store, db *sql.DB, dsn, kind, compose string, plain, secret map[string]string, services []string, n8n bool, scopes ...map[string][]string) {
	t.Helper()
	project := "cooker-e2e-" + kind + "-" + uuid.NewString()[:8]
	source := t.TempDir()
	composeFile := filepath.Join(source, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte(compose), 0600); err != nil {
		t.Fatal(err)
	}
	liveGit(t, source, "init", "--initial-branch=main")
	liveGit(t, source, "add", "compose.yaml")
	liveGit(t, source, "-c", "user.name=Cooker Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "reviewed fixture")
	original := strings.TrimSpace(liveGit(t, source, "rev-parse", "HEAD"))
	t.Cleanup(func() { liveRemoveProject(t, project) })
	envID := uuid.NewString()
	runtimePlain := plain
	if len(scopes) > 0 {
		runtimePlain = nil
	}
	if err := st.Environments.Create(ctx, &model.Environment{ID: envID, Name: project, PlainVars: runtimePlain}); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	codec, err := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	secrets := database.New(st.Environments, codec)
	for k, v := range secret {
		if err := secrets.Put(ctx, envID, k, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	app := &model.App{ID: uuid.NewString(), Name: project, GitHubRepo: "fixture/" + kind, Branch: "main", EnvironmentID: envID, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Path: "compose.yaml"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: project}}
	if len(scopes) > 0 {
		app.BuildPlan.EnvironmentScope = ServiceEnvironmentScope
		app.BuildPlan.ServiceEnvironment = scopes[0]
		app.BuildPlan.Variables = plain
	}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	logs := logstore.NewPostgres(db, logstore.Limits{})
	e := NewExecutor(WithComposeDeployer(deployer.NewCompose()), WithLogStore(logs), WithRunUpdater(func(ctx context.Context, r *model.PipelineRun) error {
		return st.Runs.UpdateProgress(ctx, r.ID, r.StageRuns)
	}))
	d := NewAppDeployer(e, "")
	d.Apps, d.Deploys = st.Apps, st.AppDeploys
	d.EnvResolver = &AppEnvResolver{Environments: st.Environments, Secrets: secrets}
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
	d.CheckExecution = func(_ *model.App, builds bool) error {
		if builds {
			return fmt.Errorf("image-only fixture unexpectedly required a build")
		}
		return nil
	}
	d.cloneFn = func(ctx context.Context, opts github.CloneOptions) (string, error) {
		if opts.Commit != original {
			return "", fmt.Errorf("worker did not preserve accepted commit")
		}
		clone, err := os.MkdirTemp("", "cooker-live-checkout-")
		if err != nil {
			return "", err
		}
		for _, args := range [][]string{{"clone", "--no-hardlinks", source, clone}, {"-C", clone, "checkout", "--detach", opts.Commit}} {
			cmd := exec.CommandContext(ctx, "git", args...)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
			if err := cmd.Run(); err != nil {
				os.RemoveAll(clone)
				return "", err
			}
		}
		return clone, nil
	}
	jobs := jobqueue.NewPostgres(db)
	jobs.OnTransition = storepg.SyncJobRun
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, Deadline: 6 * time.Minute, ResolveCommit: func(context.Context, int64, string, string) (string, error) { return original, nil }}
	q.RequireImagePins = true
	q.PrepareImages = func(ctx context.Context, plan *AppDeployPlan) (*AppImageResolution, error) {
		return d.PrepareImages(ctx, plan, RegistryImageResolver(nil))
	}
	receipt, err := q.Enqueue(ctx, app, "", &model.PipelineRun{StartedByUserSub: "local-acceptance"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Commit != original {
		t.Fatal("accepted source SHA changed")
	}
	// Move the source branch before the worker starts. A wrong implementation
	// would now try the deliberately invalid image and cannot pass this fixture.
	if err := os.WriteFile(composeFile, []byte("services:\n  changed:\n    image: invalid.invalid/must-not-be-deployed:changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	liveGit(t, source, "add", "compose.yaml")
	liveGit(t, source, "-c", "user.name=Cooker Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "move branch after acceptance")
	reg := jobqueue.NewRegistry()
	reg.Register(JobKindAppDeploy, q.Handle)
	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{Store: jobs, Registry: reg, Size: 2, PollEvery: 50 * time.Millisecond, HeartbeatEvery: time.Second, LeaseTimeout: 10 * time.Second, OnHeartbeat: HeartbeatJobRun(st), OnRecovery: RecoverJobRun(st), ClaimContext: func(ctx context.Context, j *jobqueue.Job) context.Context {
		return store.WithRunClaim(ctx, j.ID, j.LockedBy, nil)
	}})
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- pool.Run(workerCtx) }()
	defer func() { stop(); <-done }()
	var job *jobqueue.Job
	for {
		job, err = jobs.Get(ctx, receipt.JobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != jobqueue.StatusPending && job.Status != jobqueue.StatusRunning {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
	run, err := st.Runs.Get(ctx, receipt.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != jobqueue.StatusSucceeded || run.Status != model.RunStatusSuccess {
		t.Errorf("job=%s run=%s error=%s", job.Status, run.Status, run.Error)
		for _, s := range run.StageRuns {
			t.Logf("stage=%s status=%s error=%s logs=%s", s.StageID, s.Status, s.Error, s.Logs)
		}
		t.FailNow()
	}
	graph, err := st.Pipelines.Get(ctx, receipt.PipelineID)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Stages) != len(services) {
		t.Fatalf("unexpected deployment stage count %d", len(graph.Stages))
	}
	if n8n && len(graph.Edges) != 5 {
		t.Fatalf("n8n dependency count=%d", len(graph.Edges))
	}
	// Read using a fresh SQL handle/log store: replay cannot depend on the
	// executor's in-memory log buffer.
	replayDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer replayDB.Close()
	replay := logstore.NewPostgres(replayDB, logstore.Limits{})
	runtime := NewRuntimeService("")
	runtime.Store = st
	runtime.SecretGenerationBound = true // database manager writes Environment.Version
	for _, service := range services {
		status, err := runtime.Status(ctx, app, service)
		if err != nil || !status.Healthy {
			t.Fatalf("service=%s runtime=%+v error=%v", service, status, err)
		}
		if len(scopes) > 0 {
			liveCheckServiceSecrets(t, ctx, app, service, scopes[0][service], secret)
		}
		entries, err := replay.Read(ctx, receipt.RunID, "deploy-"+sanitize(service), 0)
		if err != nil || len(entries) == 0 {
			t.Fatalf("no durable deployment logs for %s: %v", service, err)
		}
		logCtx, logCancel := context.WithTimeout(ctx, 750*time.Millisecond)
		var out bytes.Buffer
		_ = runtime.StreamDeploymentLogs(logCtx, app, service, jobs, d.EnvResolver, &out)
		logCancel()
		if out.Len() == 0 {
			t.Fatalf("service logs empty for %s", service)
		}
		for _, value := range secret {
			if bytes.Contains(out.Bytes(), []byte(value)) {
				t.Fatalf("resolved secret leaked into service logs for %s", service)
			}
		}
	}
	for _, value := range secret {
		b, _ := json.Marshal(run)
		g, _ := json.Marshal(graph)
		j, _ := jobs.Get(ctx, receipt.JobID)
		if bytes.Contains(b, []byte(value)) || bytes.Contains(g, []byte(value)) || bytes.Contains(j.Payload, []byte(value)) {
			t.Fatal("secret leaked into persisted execution evidence")
		}
	}
	if n8n {
		cmd := exec.CommandContext(ctx, "python3", "testdata/n8n_queue_smoke.py", "http://127.0.0.1:"+plain["N8N_PORT"], project)
		result, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("n8n queue proof: %v %s", err, result)
		}
		t.Logf("n8n worker proof: %s", result)
	} else {
		cmd := exec.CommandContext(ctx, "docker", "exec", runtimeWorkloadName(app, "redis"), "redis-cli", "ping")
		result, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(result)) != "PONG" {
			t.Fatalf("Redis command failed: %v", err)
		}
	}
	t.Logf("verified app=%s run=%s commit=%s services=%d durable-stage-logs=yes service-logs=yes", project, receipt.RunID, original, len(services))
}

// Only inspect this fixture's exact container. Values stay in memory; failures
// identify the key/service without printing Docker inspect or credentials.
func liveCheckServiceSecrets(t *testing.T, ctx context.Context, app *model.App, service string, allowed []string, secrets map[string]string) {
	t.Helper()
	if !strings.HasPrefix(app.DeployTarget.Prefix, "cooker-e2e-n8n-") {
		t.Fatal("unexpected acceptance project")
	}
	data, err := exec.CommandContext(ctx, "docker", "inspect", runtimeWorkloadName(app, service), "--format", "{{json .Config.Env}}").Output()
	if err != nil {
		t.Fatalf("inspect fixture environment for %s: %v", service, err)
	}
	var entries []string
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("decode fixture environment: %v", err)
	}
	values := map[string]string{}
	for _, entry := range entries {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, secret := range secrets {
		expected := slices.Contains(allowed, key)
		if expected && values[key] != secret {
			t.Fatalf("service %s did not receive allowed key %s", service, key)
		}
		for _, value := range values {
			if !expected && strings.Contains(value, secret) {
				t.Fatalf("service %s received unallowed key %s or an alias", service, key)
			}
		}
	}
	for _, key := range []string{"N8N_IMAGE", "POSTGRES_IMAGE", "REDIS_IMAGE"} {
		if _, present := values[key]; present {
			t.Fatalf("service %s received interpolation-only input %s", service, key)
		}
	}
	t.Logf("verified runtime scope service=%s allowed-secret-keys=%v unbound-secret=absent interpolation-images=absent", service, allowed)
}

func liveRandom(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
func liveDockerImage(t *testing.T, ctx context.Context, tag string) string {
	t.Helper()
	b, err := exec.CommandContext(ctx, "docker", "image", "inspect", tag, "--format", "{{index .RepoDigests 0}}").Output()
	if err != nil {
		t.Fatalf("cached fixture image %s unavailable: %v", tag, err)
	}
	return strings.TrimSpace(string(b))
}
func liveGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null", "-C", dir}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("local source fixture: %v %s", err, b)
	}
	return string(b)
}
func liveRemoveProject(t *testing.T, project string) {
	t.Helper()
	if !strings.HasPrefix(project, "cooker-e2e-") {
		t.Error("invalid cleanup project")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for _, kind := range []struct {
		list   []string
		remove []string
	}{
		{[]string{"ps", "-aq", "--filter", "label=com.docker.compose.project=" + project}, []string{"rm", "-f", "-v"}},
		{[]string{"volume", "ls", "-q", "--filter", "label=com.docker.compose.project=" + project}, []string{"volume", "rm"}},
		{[]string{"network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + project}, []string{"network", "rm"}},
	} {
		b, err := exec.CommandContext(ctx, "docker", kind.list...).Output()
		if err != nil {
			t.Errorf("fixture cleanup list: %v", err)
			continue
		}
		ids := strings.Fields(string(b))
		if len(ids) > 0 {
			cmd := exec.CommandContext(ctx, "docker", append(kind.remove, ids...)...)
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			if err = cmd.Run(); err != nil {
				t.Errorf("fixture cleanup failed: %v", err)
			}
		}
		b, err = exec.CommandContext(ctx, "docker", kind.list...).Output()
		if err != nil || len(bytes.TrimSpace(b)) > 0 {
			t.Errorf("owned resources remain after cleanup: %v", err)
		}
	}
}
