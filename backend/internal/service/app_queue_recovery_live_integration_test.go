//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/source/gittransport"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

type recoveryLiveConfig struct {
	DSN     string `json:"dsn"`
	Source  string `json:"source"`
	Commit  string `json:"commit"`
	Marker  string `json:"marker"`
	TempDir string `json:"tempDir"`
}

type recoveryAfterMutation struct {
	actual deployer.Deployer
	marker string
}

func (d recoveryAfterMutation) Deploy(ctx context.Context, req deployer.Request) (deployer.Result, error) {
	result, err := d.actual.Deploy(ctx, req)
	if err != nil {
		return result, err
	}
	// The real adapter has created the workload and completed its readiness
	// check. Simulate process loss before this mutation's success is recorded.
	if err := os.WriteFile(d.marker, []byte(req.Name), 0600); err != nil {
		return result, err
	}
	<-ctx.Done()
	return result, ctx.Err()
}

func recoveryLiveQueue(t *testing.T, ctx context.Context, cfg recoveryLiveConfig, st *store.Store, db *sql.DB, adapter deployer.Deployer) (*AppDeployQueue, *jobqueue.Postgres) {
	t.Helper()
	e := NewExecutor(WithComposeDeployer(adapter), WithLogStore(logstore.NewPostgres(db, logstore.Limits{})), WithRunUpdater(func(ctx context.Context, r *model.PipelineRun) error {
		return st.Runs.UpdateProgress(ctx, r.ID, r.StageRuns)
	}))
	d := NewAppDeployer(e, "")
	d.Apps, d.Deploys = st.Apps, st.AppDeploys
	d.EnvResolver = &AppEnvResolver{Environments: st.Environments}
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
	d.CheckExecution = func(_ *model.App, build bool) error {
		if build {
			return fmt.Errorf("recovery fixture is image-only")
		}
		return nil
	}
	d.cloneFn = func(ctx context.Context, opts github.CloneOptions) (string, error) {
		if opts.Commit != cfg.Commit {
			return "", fmt.Errorf("wrong accepted source revision")
		}
		dir, err := os.MkdirTemp(cfg.TempDir, "cooker-recovery-checkout-")
		if err != nil {
			return "", err
		}
		for _, args := range [][]string{{"-c", "protocol.file.allow=always", "clone", "--no-local", cfg.Source, dir}, {"-C", dir, "checkout", "--detach", opts.Commit}} {
			cmd := exec.CommandContext(ctx, "git", args...)
			cmd.Env = gittransport.Environment("/nonexistent")
			if err := cmd.Run(); err != nil {
				_ = os.RemoveAll(dir)
				return "", err
			}
		}
		return dir, nil
	}
	jobs := jobqueue.NewPostgres(db)
	jobs.OnTransition = storepg.SyncJobRun
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, Deadline: 3 * time.Minute, RequireImagePins: true, ResolveCommit: func(context.Context, int64, string, string) (string, error) { return cfg.Commit, nil }}
	q.PrepareImages = func(ctx context.Context, p *AppDeployPlan) (*AppImageResolution, error) {
		return d.PrepareImages(ctx, p, RegistryImageResolver(nil))
	}
	return q, jobs
}

func recoveryLivePool(t *testing.T, jobs jobqueue.Store, st *store.Store, handle func(context.Context, *jobqueue.Job) error) *jobqueue.Pool {
	t.Helper()
	reg := jobqueue.NewRegistry()
	reg.Register(JobKindAppDeploy, handle)
	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{Store: jobs, Registry: reg, Size: 1, WorkerIDPrefix: "recovery-fixture", PollEvery: 50 * time.Millisecond, HeartbeatEvery: 250 * time.Millisecond, LeaseTimeout: 2 * time.Second, OnHeartbeat: HeartbeatJobRun(st), OnRecovery: RecoverJobRun(st), ClaimContext: func(ctx context.Context, j *jobqueue.Job) context.Context {
		return store.WithRunClaim(ctx, j.ID, j.LockedBy, nil)
	}})
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// TestAppQueueRecoveryWorker is only invoked as the disposable worker child.
// The parent kills it after observing the actual Docker side effect.
func TestAppQueueRecoveryWorker(t *testing.T) {
	path := os.Getenv("COOKER_RECOVERY_WORKER_CONFIG")
	if path == "" {
		t.Skip("worker helper")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg recoveryLiveConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(cfg.DSN)
	if err != nil || u.Hostname() != "127.0.0.1" || !strings.HasPrefix(u.Path, "/cooker_acceptance_recovery_") {
		t.Fatal("invalid worker fixture database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	st, err := storepg.NewStore(ctx, cfg.DSN)
	if err != nil {
		t.Fatal("worker cannot open fixture store")
	}
	defer st.Close()
	db, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q, jobs := recoveryLiveQueue(t, ctx, cfg, st, db, recoveryAfterMutation{actual: deployer.NewCompose(), marker: cfg.Marker})
	if err := recoveryLivePool(t, jobs, st, q.Handle).Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAppQueueLiveInterruptedWorkerRecovery(t *testing.T) {
	if os.Getenv("COOKER_RECOVERY_ACCEPTANCE") != "true" {
		t.Skip("disposable Docker recovery acceptance is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	evidence := map[string]any{"scenario": "SIGKILL after real Compose readiness, before mutation success persistence", "scope": "local Git, real PostgreSQL, real Docker Compose; recovery operation called directly, no hosted source or authentication UI"}
	evidenceDir := os.Getenv("COOKER_RECOVERY_EVIDENCE_DIR")
	t.Cleanup(func() {
		evidence["passed"] = !t.Failed()
		if evidenceDir != "" {
			if err := os.MkdirAll(evidenceDir, 0700); err != nil {
				t.Error(err)
				return
			}
			b, _ := json.MarshalIndent(evidence, "", "  ")
			if err := os.WriteFile(filepath.Join(evidenceDir, "acceptance.json"), append(b, '\n'), 0600); err != nil {
				t.Error(err)
			}
		}
	})
	id := uuid.NewString()[:8]
	pgName := "cooker-recovery-pg-" + id
	project := "cooker-e2e-recovery-" + id
	pgImage := liveDockerImage(t, ctx, "postgres:16-alpine")
	redisImage := liveDockerImage(t, ctx, "redis:7-alpine")
	password := liveRandom(t)
	database := "cooker_acceptance_recovery_" + id
	cmd := exec.CommandContext(ctx, "docker", "run", "-d", "--name", pgName, "--label", "io.cooker.acceptance=recovery", "-p", "127.0.0.1::5432", "-e", "POSTGRES_PASSWORD="+password, "-e", "POSTGRES_DB="+database, pgImage)
	if _, err := cmd.Output(); err != nil {
		t.Fatal("cannot start disposable recovery database")
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := exec.CommandContext(cleanupCtx, "docker", "rm", "-f", "-v", pgName).Run(); err != nil {
			t.Errorf("fixture database cleanup: %v", err)
		}
		if err := exec.CommandContext(cleanupCtx, "docker", "inspect", pgName).Run(); err == nil {
			t.Error("fixture database remains")
		} else {
			evidence["databaseRemoved"] = true
		}
	})
	t.Cleanup(func() { liveRemoveProject(t, project); evidence["workloadCleanupChecked"] = true })
	portOut, err := exec.CommandContext(ctx, "docker", "port", pgName, "5432/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(string(portOut)))
	if err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%s/%s?sslmode=disable", password, port, database)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	recoveryWait(t, ctx, 20*time.Second, func() bool { return db.PingContext(ctx) == nil }, "fixture database readiness")
	st, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal("cannot migrate fixture database")
	}
	defer st.Close()
	source := t.TempDir()
	compose := fmt.Sprintf("services:\n  redis:\n    image: %s\n    healthcheck:\n      test: [CMD, redis-cli, ping]\n      interval: 1s\n      timeout: 1s\n      retries: 20\n", redisImage)
	if err := os.WriteFile(filepath.Join(source, "compose.yaml"), []byte(compose), 0600); err != nil {
		t.Fatal(err)
	}
	liveGit(t, source, "init", "--initial-branch=main")
	liveGit(t, source, "add", "compose.yaml")
	liveGit(t, source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "recovery fixture")
	sha := strings.TrimSpace(liveGit(t, source, "rev-parse", "HEAD"))
	fixtureTmp := t.TempDir()
	cfg := recoveryLiveConfig{DSN: dsn, Source: source, Commit: sha, Marker: filepath.Join(fixtureTmp, "mutation-ready"), TempDir: fixtureTmp}
	app := &model.App{ID: uuid.NewString(), Name: project, GitHubRepo: "fixture/recovery", Branch: "main", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Path: "compose.yaml"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: project}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	q, jobs := recoveryLiveQueue(t, ctx, cfg, st, db, deployer.NewCompose())
	r, err := q.Enqueue(ctx, app, "", &model.PipelineRun{StartedByUserSub: "recovery-fixture-operator"})
	if err != nil {
		t.Fatal(err)
	}
	evidence["runId"], evidence["jobId"], evidence["commit"], evidence["image"] = r.RunID, r.JobID, sha, redisImage
	cfgFile := filepath.Join(t.TempDir(), "worker.json")
	cfgData, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgFile, cfgData, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestAppQueueRecoveryWorker$", "-test.v", "-test.timeout=170s")
	for _, key := range []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONTEXT", "XDG_RUNTIME_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			child.Env = append(child.Env, key+"="+value)
		}
	}
	child.Env = append(child.Env, "COOKER_RECOVERY_WORKER_CONFIG="+cfgFile, "TMPDIR="+cfg.TempDir)
	workerLog := filepath.Join(t.TempDir(), "worker.log")
	if evidenceDir != "" {
		if err := os.MkdirAll(evidenceDir, 0700); err != nil {
			t.Fatal(err)
		}
		workerLog = filepath.Join(evidenceDir, "worker.log")
	}
	output, err := os.OpenFile(workerLog, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	child.Stdout, child.Stderr = output, output
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	childDone := make(chan error, 1)
	go func() { childDone <- child.Wait() }()
	exited := false
	t.Cleanup(func() {
		if !exited {
			_ = child.Process.Kill()
			<-childDone
		}
	})
	recoveryWait(t, ctx, 45*time.Second, func() bool { _, err := os.Stat(cfg.Marker); return err == nil }, "real Compose mutation readiness")
	before, err := jobs.Get(ctx, r.JobID)
	if err != nil || before.Status != jobqueue.StatusRunning {
		t.Fatal("worker did not hold running claim", err)
	}
	container := strings.TrimSpace(string(recoveryDocker(t, ctx, "ps", "-q", "--filter", "label=com.docker.compose.project="+project, "--filter", "label=com.docker.compose.service=redis")))
	if container == "" || strings.Contains(container, "\n") {
		t.Fatal("fixture workload identity is ambiguous")
	}
	if ping := strings.TrimSpace(string(recoveryDocker(t, ctx, "exec", container, "redis-cli", "ping"))); ping != "PONG" {
		t.Fatal("real fixture workload did not respond")
	}
	label := strings.TrimSpace(string(recoveryDocker(t, ctx, "inspect", "--format", "{{index .Config.Labels \"io.cooker.run-id\"}}", container)))
	if label != r.RunID {
		t.Fatal("container was not owned by accepted run")
	}
	evidence["containerId"], evidence["workerPid"], evidence["readinessBeforeKill"] = container, child.Process.Pid, "PONG; matching run label"
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := <-childDone; err == nil {
		t.Fatal("worker was not interrupted")
	}
	exited = true
	evidence["workerKilled"] = true
	// Fresh connections and a restarted pool must reconcile rather than execute
	// the uncertain operation. A second real App job stays pending behind it.
	fresh, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal("fresh store failed")
	}
	defer fresh.Close()
	freshDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer freshDB.Close()
	freshQueue, freshJobs := recoveryLiveQueue(t, ctx, cfg, fresh, freshDB, deployer.NewCompose())
	next, err := freshQueue.Enqueue(ctx, app, sha, nil)
	if err != nil {
		t.Fatal(err)
	}
	var unexpected atomic.Int32
	pool := recoveryLivePool(t, freshJobs, fresh, func(context.Context, *jobqueue.Job) error {
		unexpected.Add(1)
		return fmt.Errorf("uncertain App must remain locked")
	})
	poolCtx, stop := context.WithCancel(ctx)
	poolDone := make(chan error, 1)
	go func() { poolDone <- pool.Run(poolCtx) }()
	poolStopped := false
	defer func() {
		if !poolStopped {
			stop()
			<-poolDone
		}
	}()
	recoveryWait(t, ctx, 10*time.Second, func() bool {
		j, err := freshJobs.Get(ctx, r.JobID)
		return err == nil && j.Status == jobqueue.StatusNeedsAttention
	}, "expired worker claim recovery")
	time.Sleep(300 * time.Millisecond)
	stop()
	<-poolDone
	poolStopped = true
	if unexpected.Load() != 0 {
		t.Fatal("restarted pool blindly executed work while App lock was uncertain")
	}
	after, _ := freshJobs.Get(ctx, r.JobID)
	run, _ := fresh.Runs.Get(ctx, r.RunID)
	pending, _ := freshJobs.Get(ctx, next.JobID)
	if run.Status != model.RunStatusNeedsAttention || after.Attempts != 1 || pending.Status != jobqueue.StatusPending {
		t.Fatal("job/run uncertainty or App lock was lost")
	}
	if err := freshJobs.Complete(ctx, before.LockedBy, r.JobID); !errors.Is(err, jobqueue.ErrNotLocked) {
		t.Fatalf("stale worker completion accepted: %v", err)
	}
	stale := run.Clone()
	stale.Status = model.RunStatusSuccess
	if err := fresh.Runs.Update(store.WithRunClaim(ctx, r.JobID, before.LockedBy, nil), stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale run write accepted: %v", err)
	}
	evidence["recoveredNeedsAttention"], evidence["heldAppLock"], evidence["attempts"], evidence["staleWorkerFenced"] = true, true, after.Attempts, true
	history, err := fresh.AppDeploys.ListByApp(ctx, app.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].RunID != r.RunID || history[0].Status != model.RunStatusNeedsAttention {
		t.Fatalf("uncertain real deployment missing matching history: count=%d", len(history))
	}
	// Fail after domain updates inside the resolution transaction: no portion
	// of the verified result may become visible on its own.
	freshJobs.OnTransition = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		if err := storepg.SyncJobRun(ctx, tx, j); err != nil {
			return err
		}
		return fmt.Errorf("injected resolution transaction failure")
	}
	proof := "Verified by fixture administrator: exact run container is healthy and redis-cli ping returned PONG"
	if err := freshJobs.Resolve(ctx, r.JobID, jobqueue.StatusSucceeded, proof); err == nil {
		t.Fatal("injected resolution failure committed")
	}
	after, _ = freshJobs.Get(ctx, r.JobID)
	run, _ = fresh.Runs.Get(ctx, r.RunID)
	history, _ = fresh.AppDeploys.ListByApp(ctx, app.ID, 10)
	if after.Status != jobqueue.StatusNeedsAttention || run.Status != model.RunStatusNeedsAttention || len(history) != 1 || history[0].Status != model.RunStatusNeedsAttention {
		t.Fatal("administrator resolution committed partially")
	}
	freshJobs.OnTransition = storepg.SyncJobRun
	if err := freshJobs.Resolve(ctx, r.JobID, jobqueue.StatusSucceeded, proof); err != nil {
		t.Fatal(err)
	}
	after, _ = freshJobs.Get(ctx, r.JobID)
	run, _ = fresh.Runs.Get(ctx, r.RunID)
	history, _ = fresh.AppDeploys.ListByApp(ctx, app.ID, 10)
	if after.Status != jobqueue.StatusSucceeded || run.Status != model.RunStatusSuccess || run.FinishedAt == nil || len(history) != 1 || history[0].Status != model.RunStatusSuccess || after.LastError != proof || run.Error != proof {
		t.Fatal("job/run/history resolution diverged")
	}
	if err := freshJobs.Resolve(ctx, r.JobID, jobqueue.StatusFailed, "competing stale administrator resolution"); !errors.Is(err, jobqueue.ErrNotLocked) {
		t.Fatal("stale administrator overwrote resolution", err)
	}
	claimed, err := freshJobs.Dequeue(ctx, "post-resolution-fixture", []string{JobKindAppDeploy})
	if err != nil || claimed == nil || claimed.ID != next.JobID {
		t.Fatal("verified outcome did not release App lock", err)
	}
	if _, err := freshJobs.Cancel(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	if err := freshJobs.Resolve(ctx, claimed.ID, jobqueue.StatusCancelled, "fixture cleanup: second job was claimed but never dispatched"); err != nil {
		t.Fatal(err)
	}
	still := strings.TrimSpace(string(recoveryDocker(t, ctx, "ps", "-q", "--filter", "label=com.docker.compose.project="+project, "--filter", "label=com.docker.compose.service=redis")))
	if still != container {
		t.Fatal("recovery recreated the workload")
	}
	evidence["atomicResolutionRollback"], evidence["resolvedJobRunHistoryTogether"], evidence["appLockReleased"], evidence["workloadNotRepeated"] = true, true, true, true
	if binary := os.Getenv("COOKER_RECOVERY_HISTORY_TEST_BINARY"); binary != "" {
		regression := exec.CommandContext(ctx, binary, "-test.run=^TestAppRecoveryHistoryPreservesEvidenceWithoutInventingRuntimeSuccess$", "-test.v")
		regression.Env = []string{"PATH=" + os.Getenv("PATH"), "COOKER_INTEGRATION_DATABASE_URL=" + dsn}
		output, err := regression.CombinedOutput()
		if evidenceDir != "" {
			if writeErr := os.WriteFile(filepath.Join(evidenceDir, "history-regression.log"), output, 0600); writeErr != nil {
				t.Fatal(writeErr)
			}
		}
		if err != nil {
			t.Fatalf("history regression failed: %s", output)
		}
		evidence["historyRegressionPassed"] = true
	}
	t.Logf("recovery accepted run=%s source=%s killed-worker=%d container=%s needs-attention=verified stale-claim=fenced resolution=atomic lock=released", r.RunID, sha, child.Process.Pid, container)
}

func recoveryWait(t *testing.T, ctx context.Context, timeout time.Duration, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("timed out waiting for " + description)
}
func recoveryDocker(t *testing.T, ctx context.Context, args ...string) []byte {
	t.Helper()
	b, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture Docker operation %v: %s", err, b)
	}
	return b
}
