package service

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/model"
)

// deployRecordFromRun derives the history row for a finished deploy.
// Single-image path: image_ref is the synthesized tag; digest comes
// from the build stage's artifacts when the builder reported one.
// Compose path (multi-image): refs stay empty — not rollback-eligible.
func deployRecordFromRun(app *model.App, p *model.Pipeline, run *model.PipelineRun, imageRef string, kind model.AppDeployKind) *model.AppDeploy {
	rec := &model.AppDeploy{
		ID:       uuid.New().String(),
		AppID:    app.ID,
		ImageRef: imageRef,
		Kind:     kind,
	}
	if p != nil {
		rec.PipelineID = p.ID
	}
	if run != nil {
		rec.RunID = run.ID
		rec.Status = run.Status
		for i := range run.StageRuns {
			for _, a := range run.StageRuns[i].Artifacts {
				if a.Type == "oci-image" && a.Digest != "" {
					rec.Digest = a.Digest
				}
			}
		}
	}
	return rec
}

// stripDeployStage returns the pipeline/run with any deploy stage (and
// edges referencing it) removed, so BuildAndPushImage runs build+push
// only. synthesizePipeline appends the deploy stage last for K8s
// targets; for canary we deploy via the weighted deployer instead.
func stripDeployStage(p *model.Pipeline, run *model.PipelineRun) (*model.Pipeline, *model.PipelineRun) {
	keptStages := make([]model.Stage, 0, len(p.Stages))
	dropped := make(map[string]bool)
	for _, s := range p.Stages {
		if s.Type == model.StageTypeDeploy {
			dropped[s.ID] = true
			continue
		}
		keptStages = append(keptStages, s)
	}
	keptEdges := make([]model.Edge, 0, len(p.Edges))
	for _, e := range p.Edges {
		if dropped[e.Source] || dropped[e.Target] {
			continue
		}
		keptEdges = append(keptEdges, e)
	}
	p.Stages, p.Edges = keptStages, keptEdges

	keptRuns := make([]model.StageRun, 0, len(run.StageRuns))
	for _, sr := range run.StageRuns {
		if dropped[sr.StageID] {
			continue
		}
		keptRuns = append(keptRuns, sr)
	}
	run.StageRuns = keptRuns
	return p, run
}

// synthesizePipeline builds the four-stage Clone→Build→Push→Deploy
// DAG for an App deploy. Clone already ran by the time we call this,
// so Stage 1 ("Checkout") is marked succeeded and left as a record.
func synthesizePipeline(app *model.App, plan *model.BuildPlan, workdir, tag string, cache *model.CacheSpec) (*model.Pipeline, *model.PipelineRun) {
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
				Cache:      cache,
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
func synthesizePipelineFromCompose(app *model.App, graph *model.ComposeGraph, workdir, registry string, ts int64, pipelineID, runID string, cache *model.CacheSpec) (*model.Pipeline, *model.PipelineRun, error) {
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
	// edgeID returns a guaranteed-unique edge ID. Slugs contain '-', so
	// concatenating them with a '-' separator is ambiguous (two distinct
	// src/tgt pairs can collide). A monotonic counter sidesteps the
	// ambiguity entirely — the source/target fields carry the semantics.
	edgeSeq := 0
	edgeID := func() string {
		edgeSeq++
		return fmt.Sprintf("e%d", edgeSeq)
	}

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
						Cache:               cache,
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
			edges = append(edges, model.Edge{ID: edgeID(), Source: buildID, Target: pushID})
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
			edges = append(edges, model.Edge{ID: edgeID(), Source: "push-" + slug, Target: deployID})
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
			edges = append(edges, model.Edge{ID: edgeID(), Source: depDeploy, Target: src})
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
