package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
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
	workdir, err := github.Clone(ctx, github.CloneOptions{
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
		p, run, synthErr = synthesizePipelineFromCompose(app, graph, workdir, registry, ts, pipelineID, runID)
		if synthErr != nil {
			return nil, nil, synthErr
		}
	} else {
		p, run = synthesizePipeline(app, plan, workdir, tag)
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
	if _, err := d.Executor.Execute(ctx, p, run); err != nil {
		return p, run, fmt.Errorf("execute: %w", err)
	}
	fmt.Fprintf(logW, "[done] run=%s status=%s\n", run.ID, run.Status)
	return p, run, nil
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

// ComposePipelineID returns the deterministic pipeline ID for a
// compose deploy so the handler (which knows runID before Deploy runs)
// and the synthesizer agree. Prefers the runID (unique per deploy);
// falls back to app+timestamp when runID is empty (coordinator-less
// callers).
func ComposePipelineID(runID, appID string, ts int64) string {
	if runID != "" {
		return "app-" + appID + "-" + runID
	}
	return fmt.Sprintf("app-%s-%d", appID, ts)
}

// synthesizePipelineFromCompose turns a parsed ComposeGraph into a
// per-service deployment DAG: each service gets a build→push→deploy
// sub-chain (build/push skipped for image-only services), and a
// service's deploy waits on its dependencies' deploys (depends_on).
// Stages carry the service's Group (for group-box rendering), its
// resource limits, and a DeployRuntime selected from the App's target.
//
// pipelineID is supplied by the caller so the synthesized pipeline can
// be persisted and fetched (see handler.runAppDeployCtx). Returns an
// error if the depends_on graph contains a cycle.
func synthesizePipelineFromCompose(app *model.App, graph *model.ComposeGraph, workdir, registry string, ts int64, pipelineID, runID string) (*model.Pipeline, *model.PipelineRun, error) {
	runtime := deployRuntimeFor(app.DeployTarget.Kind)

	// Assign each service a unique, sanitized slug for stage IDs,
	// disambiguating collisions (two service names → same slug).
	slugOf := make(map[string]string, len(graph.Services))
	usedSlug := make(map[string]bool)
	for _, svc := range graph.Services {
		base := sanitize(svc.Name)
		slug := base
		for i := 2; usedSlug[slug]; i++ {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
		usedSlug[slug] = true
		slugOf[svc.Name] = slug
	}

	var stages []model.Stage
	var edges []model.Edge
	deployStageID := make(map[string]string, len(graph.Services))

	for _, svc := range graph.Services {
		slug := slugOf[svc.Name]
		hasBuild := svc.Build != nil
		deployImage := svc.Image // image-only services deploy as-is

		if hasBuild {
			bctx := "."
			df := "Dockerfile"
			if svc.Build.Context != "" {
				bctx = svc.Build.Context
			}
			if svc.Build.Dockerfile != "" {
				df = svc.Build.Dockerfile
			}
			// Guard against path escape in operator-supplied context/df
			// (mirrors the single-service guard in synthesizePipeline).
			cleanCtx := filepath.Clean(bctx)
			if filepath.IsAbs(cleanCtx) || cleanCtx == ".." || strings.HasPrefix(cleanCtx, ".."+string(filepath.Separator)) {
				cleanCtx = "."
			}
			cleanDf := filepath.Clean(df)
			if filepath.IsAbs(cleanDf) || cleanDf == ".." || strings.HasPrefix(cleanDf, ".."+string(filepath.Separator)) {
				cleanDf = "Dockerfile"
			}
			deployImage = fmt.Sprintf("%s/%s-%s:%d", registry, sanitize(app.Name), slug, ts)

			buildID := "build-" + slug
			pushID := "push-" + slug
			stages = append(stages,
				model.Stage{
					ID: buildID, Name: "Build " + svc.Name, Type: model.StageTypeBuild, Group: svc.Group,
					Config: model.StageConfig{
						Dockerfile:          cleanDf,
						Context:             filepath.Join(workdir, cleanCtx),
						Tags:                []string{deployImage},
						ComposeServiceName:  svc.Name,
						ComposeBuildContext: cleanCtx,
						ComposeDockerfile:   cleanDf,
					},
				},
				model.Stage{
					ID: pushID, Name: "Push " + svc.Name, Type: model.StageTypePush, Group: svc.Group,
					Config: model.StageConfig{
						Image:              deployImage,
						Repository:         deployImage,
						ComposeServiceName: svc.Name,
					},
				},
			)
			edges = append(edges, model.Edge{ID: "e-" + buildID + "-" + pushID, Source: buildID, Target: pushID})
		}

		deployID := "deploy-" + slug
		deployStageID[svc.Name] = deployID
		deployCfg := model.StageConfig{
			DeployRuntime:      string(runtime),
			ComposeServiceName: svc.Name,
			Resources:          svc.Resources,
			Image:              deployImage,
			// Carry the service's ports + env so a docker-run deploy can
			// publish/inject them. K8s deploys read the manifest instead.
			ComposePorts: svc.Ports,
			Env:          svc.Environment,
		}
		if runtime == deployRuntimeKubernetes {
			deployCfg.Namespace = app.DeployTarget.Namespace
			deployCfg.ManifestPath = composeServiceManifest(&svc, deployImage)
		}
		stages = append(stages, model.Stage{
			ID: deployID, Name: "Deploy " + svc.Name, Type: model.StageTypeDeploy, Group: svc.Group,
			Config: deployCfg,
		})
		if hasBuild {
			edges = append(edges, model.Edge{ID: "e-push-" + slug + "-" + deployID, Source: "push-" + slug, Target: deployID})
		}
	}

	// Cross-service edges: deploy-<svc> waits on deploy-<dep>.
	for _, svc := range graph.Services {
		for _, dep := range svc.DependsOn {
			depDeploy, ok := deployStageID[dep]
			if !ok {
				slog.Warn("compose synth: depends_on names unknown service", "service", svc.Name, "dep", dep)
				continue
			}
			src := deployStageID[svc.Name]
			edges = append(edges, model.Edge{ID: "e-dep-" + slugOf[dep] + "-" + slugOf[svc.Name], Source: depDeploy, Target: src})
		}
	}

	now := time.Now()
	p := &model.Pipeline{
		ID:        pipelineID,
		Name:      "Deploy " + app.Name,
		Stages:    stages,
		Edges:     edges,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Surface depends_on cycles with a friendly error before dispatch
	// (the executor would also reject them, but later and less clearly).
	if _, err := BuildDAGFromPipeline(p); err != nil {
		return nil, nil, fmt.Errorf("compose deploy graph invalid: %w", err)
	}

	rid := runID
	if rid == "" {
		rid = uuid.New().String()
	}
	run := &model.PipelineRun{
		ID:         rid,
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns:  make([]model.StageRun, 0, len(stages)),
	}
	for _, s := range stages {
		run.StageRuns = append(run.StageRuns, model.StageRun{StageID: s.ID, Status: model.RunStatusPending})
	}
	return p, run, nil
}

// deployRuntime selects how a synthesized deploy stage runs.
type deployRuntime string

const (
	deployRuntimeKubernetes deployRuntime = "kubernetes"
	deployRuntimeDocker     deployRuntime = "docker"
	deployRuntimeCompose    deployRuntime = "compose"
)

// deployRuntimeFor maps an App's deploy-target kind to the per-service
// deploy runtime. Kubernetes → manifest apply; docker-host → per-
// service docker run. Other targets default to kubernetes-manifest
// semantics for now.
func deployRuntimeFor(kind model.DeployTargetKind) deployRuntime {
	switch kind {
	case model.DeployTargetDockerHost:
		return deployRuntimeDocker
	default:
		return deployRuntimeKubernetes
	}
}

// composeServiceManifest synthesises a minimal Deployment + Service for
// one compose service, parameterised by image, first published port,
// and (optionally) resource limits. Mirrors defaultKubernetesManifest
// but per-service and resource-aware.
func composeServiceManifest(svc *model.ComposeService, image string) string {
	name := sanitize(svc.Name)
	port := firstContainerPort(svc.Ports)
	resources := k8sResourceBlock(svc.Resources)
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
          ports: [{containerPort: %[3]d}]%[4]s
---
apiVersion: v1
kind: Service
metadata:
  name: %[1]s
spec:
  selector: {app: %[1]s}
  ports: [{port: %[3]d, targetPort: %[3]d}]
`, name, image, port, resources)
}

// k8sResourceBlock renders a resources.limits YAML fragment (indented
// to sit under the container) or "" when no limits are set.
func k8sResourceBlock(r *model.ResourceLimits) string {
	if r == nil || (r.Memory == "" && r.CPUs == "") {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n          resources:\n            limits:")
	if r.Memory != "" {
		b.WriteString(fmt.Sprintf("\n              memory: %q", k8sMemory(r.Memory)))
	}
	if r.CPUs != "" {
		b.WriteString(fmt.Sprintf("\n              cpu: %q", r.CPUs))
	}
	return b.String()
}

// k8sMemory maps a compose memory string to a K8s quantity. Compose
// uses b/k/m/g; K8s uses Ki/Mi/Gi. Bare numbers pass through.
func k8sMemory(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasSuffix(low, "gb"), strings.HasSuffix(low, "g"):
		return strings.TrimSuffix(strings.TrimSuffix(low, "b"), "g") + "Gi"
	case strings.HasSuffix(low, "mb"), strings.HasSuffix(low, "m"):
		return strings.TrimSuffix(strings.TrimSuffix(low, "b"), "m") + "Mi"
	case strings.HasSuffix(low, "kb"), strings.HasSuffix(low, "k"):
		return strings.TrimSuffix(strings.TrimSuffix(low, "b"), "k") + "Ki"
	default:
		return s
	}
}

// firstContainerPort parses the container side of the first compose
// port mapping ("8080:80" → 80, "80" → 80). Defaults to 80.
func firstContainerPort(ports []string) int {
	for _, p := range ports {
		spec := p
		if i := strings.LastIndex(p, ":"); i >= 0 {
			spec = p[i+1:]
		}
		spec = strings.SplitN(spec, "/", 2)[0] // strip "/tcp"
		if n, err := strconv.Atoi(strings.TrimSpace(spec)); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	return 80
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
