package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cooker-ci/cooker/internal/builder"
	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/pkg/dagrunner"
)

// Executor runs pipelines using the DAG runner and reports progress.
// Production wiring injects a Builder; tests inject a mock.
type Executor struct {
	builder builder.Builder
	// Future deps: pusher, deployer, ws hub.
}

// Option configures a new Executor. Use the With* constructors.
type Option func(*Executor)

// WithBuilder injects the image builder used by build stages.
// Passing nil is a no-op (the default Noop builder is kept).
func WithBuilder(b builder.Builder) Option {
	return func(e *Executor) {
		if b != nil {
			e.builder = b
		}
	}
}

// NewExecutor creates a pipeline executor. Without options it uses a
// Noop builder so Execute is safe to call in tests and dry runs.
func NewExecutor(opts ...Option) *Executor {
	e := &Executor{builder: builder.Noop{}}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute runs a pipeline and returns the completed PipelineRun.
func (e *Executor) Execute(ctx context.Context, p *model.Pipeline, run *model.PipelineRun) error {
	dag, err := BuildDAGFromPipeline(p)
	if err != nil {
		return fmt.Errorf("building DAG: %w", err)
	}

	stageMap := make(map[string]*model.Stage)
	for i := range p.Stages {
		stageMap[p.Stages[i].ID] = &p.Stages[i]
	}

	stageRunMap := make(map[string]*model.StageRun)
	for i := range run.StageRuns {
		stageRunMap[run.StageRuns[i].StageID] = &run.StageRuns[i]
	}

	now := time.Now()
	run.Status = model.RunStatusRunning
	run.StartedAt = &now

	runner := dagrunner.NewRunner(dag, func(ctx context.Context, nodeID string) error {
		stage := stageMap[nodeID]
		stageRun := stageRunMap[nodeID]

		startTime := time.Now()
		stageRun.StartedAt = &startTime
		stageRun.Status = model.RunStatusRunning

		log.Printf("[pipeline:%s] executing stage %s (%s)", p.ID, stage.Name, stage.Type)

		var stageErr error
		switch stage.Type {
		case model.StageTypeBuild:
			stageErr = e.executeBuild(ctx, stage, stageRun)
		case model.StageTypeTest:
			stageErr = e.executeTest(ctx, stage)
		case model.StageTypePush:
			stageErr = e.executePush(ctx, stage)
		case model.StageTypeDeploy:
			stageErr = e.executeDeploy(ctx, stage)
		case model.StageTypeApproval:
			stageErr = e.executeApproval(ctx, stage)
		case model.StageTypeCustom:
			stageErr = e.executeCustom(ctx, stage)
		default:
			stageErr = fmt.Errorf("unknown stage type: %s", stage.Type)
		}

		endTime := time.Now()
		stageRun.FinishedAt = &endTime

		if stageErr != nil {
			stageRun.Status = model.RunStatusFailed
			stageRun.Error = stageErr.Error()
			return stageErr
		}

		stageRun.Status = model.RunStatusSuccess
		return nil
	})

	// Drain status updates (in production, these go to WebSocket)
	go func() {
		for update := range runner.Updates() {
			log.Printf("[pipeline:%s] stage %s -> %s", p.ID, update.NodeID, update.Status)
		}
	}()

	err = runner.Run(ctx)

	finishTime := time.Now()
	run.FinishedAt = &finishTime

	if err != nil {
		run.Status = model.RunStatusFailed
		run.Error = err.Error()
		return err
	}

	run.Status = model.RunStatusSuccess
	return nil
}

func (e *Executor) executeBuild(ctx context.Context, stage *model.Stage, sr *model.StageRun) error {
	req := builder.Request{
		ContextDir: stage.Config.Context,
		Dockerfile: stage.Config.Dockerfile,
		Tags:       stage.Config.Tags,
		BuildArgs:  stage.Config.BuildArgs,
		Platforms:  stage.Config.Platforms,
	}
	res, err := e.builder.Build(ctx, req)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	for _, tag := range res.Tags {
		sr.Artifacts = append(sr.Artifacts, model.Artifact{
			Type:   "oci-image",
			Ref:    tag,
			Digest: res.ImageID,
		})
	}
	return nil
}

func (e *Executor) executeTest(ctx context.Context, stage *model.Stage) error {
	// TODO: Run test container with specified image and command
	log.Printf("  Test: image=%s command=%v", stage.Config.Image, stage.Config.Command)
	return nil
}

func (e *Executor) executePush(ctx context.Context, stage *model.Stage) error {
	// TODO: Push image to OCI registry via go-containerregistry
	log.Printf("  Push: registry=%s repository=%s", stage.Config.Registry, stage.Config.Repository)
	return nil
}

func (e *Executor) executeDeploy(ctx context.Context, stage *model.Stage) error {
	// TODO: Apply K8s manifests or Helm chart via client-go
	log.Printf("  Deploy: namespace=%s manifest=%s helm=%s",
		stage.Config.Namespace, stage.Config.ManifestPath, stage.Config.HelmChart)
	return nil
}

func (e *Executor) executeApproval(ctx context.Context, stage *model.Stage) error {
	// TODO: Wait for manual approval via WebSocket/API
	log.Printf("  Approval gate: waiting for approval")
	return nil
}

func (e *Executor) executeCustom(ctx context.Context, stage *model.Stage) error {
	// TODO: Execute custom script
	log.Printf("  Custom: script=%s timeout=%s", stage.Config.Script, stage.Config.Timeout)
	return nil
}
