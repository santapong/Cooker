package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/buildplan"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/notifier"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/store"
)

// AppDeployer orchestrates a real "Deploy" button click. It clones
// the App's GitHub repo, detects or uses the configured build plan,
// and hands a synthesised pipeline to the Executor. Build logs
// stream into a writer so callers can pipe them to a WebSocket
// channel.
type AppDeployer struct {
	Executor *Executor
	// Registry is the image registry prefix used when the App
	// doesn't override it. Example: "registry.example.com/cooker".
	Registry string
	// cloneFn abstracts the git-clone step so tests can exercise Deploy's
	// full orchestration (detect -> synthesize -> execute -> cleanup ->
	// history) without a real network call or git binary. Nil (the zero
	// value, and every production caller via NewAppDeployer) falls back
	// to github.Clone via the clone() accessor below -- no behavior
	// change for any existing caller.
	cloneFn func(ctx context.Context, opts github.CloneOptions) (string, error)
	// LogSink, when non-nil, receives Printf-style log output from
	// the deployer itself (clone/detect messages). The executor
	// writes stage logs into the returned run's stage runs.
	LogSink io.Writer
	// CacheRef, when non-empty, is the registry layer-cache ref stamped
	// onto every synthesized build stage (CacheSpec{Mode:"registry"}).
	// Wired from COOKER_BUILD_CACHE_REPO.
	CacheRef string
	// Deploys, when non-nil, receives one history row per terminal
	// deploy/rollback (roadmap M3). Best-effort: a write failure is
	// logged, never surfaced — history must not fail a deploy.
	Deploys store.AppDeployStore
	// Notifier, when non-nil, receives a deploy.succeeded /
	// deploy.failed event per terminal deploy. Best-effort, same as
	// Deploys — a channel failure never fails the deploy.
	Notifier *notifier.Dispatcher
}

// cacheSpec returns the CacheSpec for synthesized build stages, or
// nil when no cache repo is configured.
func (d *AppDeployer) cacheSpec() *model.CacheSpec {
	if d.CacheRef == "" {
		return nil
	}
	return &model.CacheSpec{Mode: "registry", Ref: d.CacheRef}
}

// NewAppDeployer builds a deployer bound to exec and registry.
func NewAppDeployer(exec *Executor, registry string) *AppDeployer {
	return &AppDeployer{Executor: exec, Registry: registry}
}

// clone resolves the configured clone function, defaulting to the real
// github.Clone. Tests set d.cloneFn to exercise Deploy's orchestration
// without a real network call or git binary.
func (d *AppDeployer) clone(ctx context.Context, opts github.CloneOptions) (string, error) {
	if d.cloneFn != nil {
		return d.cloneFn(ctx, opts)
	}
	return github.Clone(ctx, opts)
}

// recordDeploy persists one history row, best-effort.
func (d *AppDeployer) recordDeploy(ctx context.Context, rec *model.AppDeploy) {
	if d.Deploys == nil || rec == nil {
		return
	}
	if err := d.Deploys.Create(ctx, rec); err != nil {
		slog.Warn("app-deploy: record history failed", "app", rec.AppID, "run", rec.RunID, "err", err)
	}
}

// Deploy runs Clone → Build → Push → Deploy for app. It returns the
// synthesized Pipeline and PipelineRun as soon as execution finishes
// (or immediately on context cancellation). The caller persists the
// run via store.Runs (and, for the grouped compose DAG, the pipeline
// via store.Pipelines) if they want history. The returned pipeline is
// non-nil whenever synthesis succeeded, even if execution then failed.
//
// Deploy cleans up the cloned working tree before returning.
// runID, when non-empty, is used as the synthesized PipelineRun's ID so
// it aligns with the stub run row the caller created and the
// app-run:<runID> WebSocket channel. An empty runID falls back to a
// fresh UUID (used by callers without a coordinator/stub row).
func (d *AppDeployer) Deploy(ctx context.Context, app *model.App, runID string, logW io.Writer) (*model.Pipeline, *model.PipelineRun, error) {
	if app.GitHubRepo == "" {
		return nil, nil, fmt.Errorf("app %s: GitHubRepo is empty", app.ID)
	}
	logW = fanOut(logW, d.LogSink)

	fmt.Fprintf(logW, "[clone] github.com/%s @ %s\n", app.GitHubRepo, app.Branch)
	workdir, err := d.clone(ctx, github.CloneOptions{
		Repo:      app.GitHubRepo,
		Branch:    app.Branch,
		Depth:     1,
		LogWriter: logW,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("clone: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(workdir); err != nil {
			slog.Warn("app-deploy: rm workdir failed", "workdir", workdir, "err", err)
		}
	}()

	plan := app.BuildPlan
	if plan == nil {
		plan = buildplan.Detect(workdir)
		fmt.Fprintf(logW, "[plan] detected kind=%s path=%s\n", plan.Kind, plan.Path)
	} else {
		fmt.Fprintf(logW, "[plan] using override kind=%s path=%s\n", plan.Kind, plan.Path)
	}

	// Target image: <registry>/<app-name>:<short-sha>  (short-sha from
	// the clone's HEAD; for now a time-based tag keeps things simple
	// and matches what CI systems fall back to when a SHA isn't cheap
	// to get).
	registry := app.RegistryRef
	if registry == "" {
		registry = d.Registry
	}
	ts := time.Now().Unix()
	tag := fmt.Sprintf("%s/%s:%d", registry, app.Name, ts)

	var p *model.Pipeline
	var run *model.PipelineRun
	if plan.Kind == model.BuildPlanCompose {
		// Grouped per-service deployment DAG. Parse the compose file the
		// build-plan detector pointed at, then synthesize one
		// build→push→deploy sub-chain per service.
		composePath := filepath.Join(workdir, plan.Path)
		data, readErr := os.ReadFile(composePath)
		if readErr != nil {
			return nil, nil, fmt.Errorf("read compose %s: %w", plan.Path, readErr)
		}
		graph, parseErr := ParseComposeGraph(data)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parse compose: %w", parseErr)
		}
		fmt.Fprintf(logW, "[plan] compose: %d service(s) → per-service DAG\n", len(graph.Services))
		// Deterministic per-deploy pipeline ID derived from the runID so
		// the handler can return it in the 202 (the deployment view needs
		// both). runID is unique per deploy; fall back to ts when absent.
		pipelineID := ComposePipelineID(runID, app.ID, ts)
		var synthErr error
		p, run, synthErr = synthesizePipelineFromCompose(app, graph, workdir, registry, ts, pipelineID, runID, d.cacheSpec())
		if synthErr != nil {
			return nil, nil, synthErr
		}
	} else {
		p, run = synthesizePipeline(app, plan, workdir, tag, d.cacheSpec())
		if runID != "" {
			run.ID = runID
		}
	}

	// Hand the run to the executor. Log output from each stage is
	// available via the stage runs' Logs field after Execute returns.
	// F2: Execute returns a terminal RunResult; the run argument also
	// reflects it. We use run.Status (and the [done] log line below)
	// rather than the RunResult so callers that inspect run see the
	// same terminal value.
	//
	// History (M3): record the terminal outcome either way. Compose
	// deploys record with an empty image_ref (multi-image — not
	// rollback-eligible); the single-image path records the tag.
	historyRef := tag
	if plan.Kind == model.BuildPlanCompose {
		historyRef = ""
	}
	if _, err := d.Executor.Execute(ctx, p, run); err != nil {
		d.recordDeploy(ctx, deployRecordFromRun(app, p, run, historyRef, model.AppDeployKindDeploy))
		NotifyDeployOutcome(d.Notifier, app, run.ID, err.Error(), false, false)
		return p, run, fmt.Errorf("execute: %w", err)
	}
	d.recordDeploy(ctx, deployRecordFromRun(app, p, run, historyRef, model.AppDeployKindDeploy))
	NotifyDeployOutcome(d.Notifier, app, run.ID, run.Error, run.Status == model.RunStatusSuccess, false)
	fmt.Fprintf(logW, "[done] run=%s status=%s\n", run.ID, run.Status)
	return p, run, nil
}

// DeployImage re-deploys a previously shipped image (rollback, M3):
// a deploy-only single-stage pipeline — no clone, no build, no push.
// v1 scope: Kubernetes deploy targets only; the caller validates the
// source history row. The synthesized run uses runID so it aligns
// with the caller's stub row and the app-run:<runID> WS channel.
func (d *AppDeployer) DeployImage(ctx context.Context, app *model.App, imageRef, runID string, logW io.Writer) (*model.Pipeline, *model.PipelineRun, error) {
	if app.DeployTarget.Kind != model.DeployTargetKubernetes {
		return nil, nil, fmt.Errorf("rollback: deploy target %q not supported (kubernetes only)", app.DeployTarget.Kind)
	}
	if imageRef == "" {
		return nil, nil, fmt.Errorf("rollback: empty image ref")
	}
	logW = fanOut(logW, d.LogSink)
	fmt.Fprintf(logW, "[rollback] re-deploying %s\n", imageRef)

	now := time.Now()
	p := &model.Pipeline{
		ID:   "app-" + app.ID + "-rollback-" + runID,
		Name: "Rollback " + app.Name,
		Stages: []model.Stage{{
			ID: "deploy", Name: "Deploy", Type: model.StageTypeDeploy,
			Config: model.StageConfig{
				Namespace:    app.DeployTarget.Namespace,
				ManifestPath: defaultKubernetesManifest(app, imageRef),
			},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	run := &model.PipelineRun{
		ID:         runID,
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "deploy", Status: model.RunStatusPending}},
	}
	if run.ID == "" {
		run.ID = uuid.New().String()
	}

	rec := func() *model.AppDeploy {
		return deployRecordFromRun(app, p, run, imageRef, model.AppDeployKindRollback)
	}
	if _, err := d.Executor.Execute(ctx, p, run); err != nil {
		d.recordDeploy(ctx, rec())
		return p, run, fmt.Errorf("execute: %w", err)
	}
	d.recordDeploy(ctx, rec())
	fmt.Fprintf(logW, "[done] rollback run=%s status=%s\n", run.ID, run.Status)
	return p, run, nil
}

// BuildAndPushImage runs Clone → Build → Push for app (no deploy stage)
// and returns the fully-qualified image ref that was pushed. It backs
// the canary path (OR-1): the CanaryService needs the new image built
// and in the registry before it can establish a weighted split, but the
// deploy itself is done by the weighted deployer, not a deploy stage.
//
// Only the single-image (non-compose) build plan is supported — canary
// traffic-splitting is per-workload and a compose multi-service deploy
// has no single image to weight. Compose apps return an error so the
// handler can fall back to (or reject in favour of) a rolling deploy.
//
// runID aligns the synthesized run with the caller's stub row and the
// app-run:<runID> WS channel, exactly like Deploy.
func (d *AppDeployer) BuildAndPushImage(ctx context.Context, app *model.App, runID string, logW io.Writer) (imageRef string, p *model.Pipeline, run *model.PipelineRun, err error) {
	if app.GitHubRepo == "" {
		return "", nil, nil, fmt.Errorf("app %s: GitHubRepo is empty", app.ID)
	}
	logW = fanOut(logW, d.LogSink)

	fmt.Fprintf(logW, "[clone] github.com/%s @ %s\n", app.GitHubRepo, app.Branch)
	workdir, cloneErr := github.Clone(ctx, github.CloneOptions{
		Repo:      app.GitHubRepo,
		Branch:    app.Branch,
		Depth:     1,
		LogWriter: logW,
	})
	if cloneErr != nil {
		return "", nil, nil, fmt.Errorf("clone: %w", cloneErr)
	}
	defer func() {
		if rmErr := os.RemoveAll(workdir); rmErr != nil {
			slog.Warn("app-deploy: rm workdir failed", "workdir", workdir, "err", rmErr)
		}
	}()

	plan := app.BuildPlan
	if plan == nil {
		plan = buildplan.Detect(workdir)
		fmt.Fprintf(logW, "[plan] detected kind=%s path=%s\n", plan.Kind, plan.Path)
	}
	if plan.Kind == model.BuildPlanCompose {
		return "", nil, nil, fmt.Errorf("canary build: compose apps are not supported (single-image only)")
	}

	registry := app.RegistryRef
	if registry == "" {
		registry = d.Registry
	}
	ts := time.Now().Unix()
	tag := fmt.Sprintf("%s/%s:%d", registry, app.Name, ts)

	// Build the Clone→Build→Push DAG and drop the deploy stage: the
	// weighted deployer owns the deploy for a canary.
	p, run = synthesizePipeline(app, plan, workdir, tag, d.cacheSpec())
	p, run = stripDeployStage(p, run)
	if runID != "" {
		run.ID = runID
	}

	if _, execErr := d.Executor.Execute(ctx, p, run); execErr != nil {
		return "", p, run, fmt.Errorf("execute build/push: %w", execErr)
	}
	fmt.Fprintf(logW, "[built] image=%s run=%s status=%s\n", tag, run.ID, run.Status)
	return tag, p, run, nil
}
