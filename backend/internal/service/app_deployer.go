package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/buildplan"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
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
	// LogSink, when non-nil, receives Printf-style log output from
	// the deployer itself (clone/detect messages). The executor
	// writes stage logs into the returned run's stage runs.
	LogSink io.Writer
}

// NewAppDeployer builds a deployer bound to exec and registry.
func NewAppDeployer(exec *Executor, registry string) *AppDeployer {
	return &AppDeployer{Executor: exec, Registry: registry}
}

// Deploy runs Clone → Build → Push → Deploy for app. It returns
// the synthesized PipelineRun as soon as execution finishes (or
// immediately on context cancellation). The caller persists the
// run via store.Runs if they want history.
//
// Deploy cleans up the cloned working tree before returning.
func (d *AppDeployer) Deploy(ctx context.Context, app *model.App, logW io.Writer) (*model.PipelineRun, error) {
	if app.GitHubRepo == "" {
		return nil, fmt.Errorf("app %s: GitHubRepo is empty", app.ID)
	}
	logW = fanOut(logW, d.LogSink)

	fmt.Fprintf(logW, "[clone] github.com/%s @ %s\n", app.GitHubRepo, app.Branch)
	workdir, err := github.Clone(ctx, github.CloneOptions{
		Repo:      app.GitHubRepo,
		Branch:    app.Branch,
		Depth:     1,
		LogWriter: logW,
	})
	if err != nil {
		return nil, fmt.Errorf("clone: %w", err)
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
	tag := fmt.Sprintf("%s/%s:%d", registry, app.Name, time.Now().Unix())

	p, run := synthesizePipeline(app, plan, workdir, tag)

	// Hand the run to the executor. Log output from each stage is
	// available via the stage runs' Logs field after Execute returns.
	// F2: Execute returns a terminal RunResult; the run argument also
	// reflects it. We use run.Status (and the [done] log line below)
	// rather than the RunResult so callers that inspect run see the
	// same terminal value.
	if _, err := d.Executor.Execute(ctx, p, run); err != nil {
		return run, fmt.Errorf("execute: %w", err)
	}
	fmt.Fprintf(logW, "[done] run=%s status=%s\n", run.ID, run.Status)
	return run, nil
}

// synthesizePipeline builds the four-stage Clone→Build→Push→Deploy
// DAG for an App deploy. Clone already ran by the time we call this,
// so Stage 1 ("Checkout") is marked succeeded and left as a record.
func synthesizePipeline(app *model.App, plan *model.BuildPlan, workdir, tag string) (*model.Pipeline, *model.PipelineRun) {
	dockerfile := "Dockerfile"
	if plan != nil && plan.Kind == model.BuildPlanDockerfile && plan.Path != "" {
		// BuildPlan.Path is operator-supplied via App config; reject
		// absolute paths or any "../" escape so a hostile config
		// can't point Kaniko at /etc/shadow as the "Dockerfile".
		clean := filepath.Clean(plan.Path)
		if !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			dockerfile = clean
		}
	}

	stages := []model.Stage{
		{
			ID: "build", Name: "Build", Type: model.StageTypeBuild,
			Config: model.StageConfig{
				Dockerfile: dockerfile,
				Context:    workdir,
				Tags:       []string{tag},
			},
		},
		{
			ID: "push", Name: "Push", Type: model.StageTypePush,
			Config: model.StageConfig{
				Image:      tag,
				Repository: tag,
			},
		},
	}
	edges := []model.Edge{
		{ID: "e1", Source: "build", Target: "push"},
	}
	if app.DeployTarget.Kind == model.DeployTargetKubernetes {
		manifest := defaultKubernetesManifest(app, tag)
		stages = append(stages, model.Stage{
			ID: "deploy", Name: "Deploy", Type: model.StageTypeDeploy,
			Config: model.StageConfig{
				Namespace:    app.DeployTarget.Namespace,
				ManifestPath: manifest,
			},
		})
		edges = append(edges, model.Edge{ID: "e2", Source: "push", Target: "deploy"})
	}

	now := time.Now()
	p := &model.Pipeline{
		ID:        "app-" + app.ID,
		Name:      "Deploy " + app.Name,
		Stages:    stages,
		Edges:     edges,
		CreatedAt: now,
		UpdatedAt: now,
	}
	run := &model.PipelineRun{
		ID:         uuid.New().String(),
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns:  make([]model.StageRun, 0, len(stages)),
	}
	for _, s := range stages {
		run.StageRuns = append(run.StageRuns, model.StageRun{StageID: s.ID, Status: model.RunStatusPending})
	}
	return p, run
}

// defaultKubernetesManifest synthesises a minimal Deployment +
// Service so UAT can click Deploy on an App pointed at a Kubernetes
// target without first writing YAML. Real workloads override this
// via App.BuildPlan / a custom pipeline.
func defaultKubernetesManifest(app *model.App, image string) string {
	name := sanitize(app.Name)
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %[1]s
spec:
  replicas: 1
  selector:
    matchLabels: {app: %[1]s}
  template:
    metadata:
      labels: {app: %[1]s}
    spec:
      containers:
        - name: %[1]s
          image: %[2]s
          ports: [{containerPort: 80}]
---
apiVersion: v1
kind: Service
metadata:
  name: %[1]s
spec:
  selector: {app: %[1]s}
  ports: [{port: 80, targetPort: 80}]
`, name, image)
}

// sanitize returns a DNS-1123 safe slug of s. The rules here are
// intentionally conservative — we replace anything that isn't
// lowercase alphanumeric with a dash.
func sanitize(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

// fanOut returns a writer that multiplexes w and secondary. Nil
// values are skipped. Concurrent-safe only when both inputs are.
func fanOut(w, secondary io.Writer) io.Writer {
	switch {
	case w != nil && secondary != nil:
		return &mwWriter{a: w, b: secondary}
	case w != nil:
		return w
	case secondary != nil:
		return secondary
	}
	return io.Discard
}

type mwWriter struct {
	mu   sync.Mutex
	a, b io.Writer
}

func (m *mwWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.a.Write(p)
	_, _ = m.b.Write(p)
	return len(p), nil
}
