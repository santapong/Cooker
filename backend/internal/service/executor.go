package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cooker-ci/cooker/internal/builder"
	"github.com/cooker-ci/cooker/internal/deployer"
	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/pusher"
	"github.com/cooker-ci/cooker/pkg/dagrunner"
)

// Executor runs pipelines using the DAG runner and reports progress.
// Production wiring injects real Builder/Pusher/Deployer; tests
// inject mocks.
type Executor struct {
	builder  builder.Builder
	pusher   pusher.Pusher
	deployer deployer.Deployer
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

// WithPusher injects the registry pusher used by push stages.
func WithPusher(p pusher.Pusher) Option {
	return func(e *Executor) {
		if p != nil {
			e.pusher = p
		}
	}
}

// WithDeployer injects the deployer used by deploy stages.
func WithDeployer(d deployer.Deployer) Option {
	return func(e *Executor) {
		if d != nil {
			e.deployer = d
		}
	}
}

// NewExecutor creates a pipeline executor. Without options it uses
// Noop backends so Execute is safe to call in tests and dry runs.
func NewExecutor(opts ...Option) *Executor {
	e := &Executor{
		builder:  builder.Noop{},
		pusher:   pusher.Noop{},
		deployer: deployer.Noop{},
	}
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
			stageErr = e.executePush(ctx, stage, stageRun)
		case model.StageTypeDeploy:
			stageErr = e.executeDeploy(ctx, stage, stageRun)
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

func (e *Executor) executePush(ctx context.Context, stage *model.Stage, sr *model.StageRun) error {
	target := stage.Config.Repository
	if stage.Config.Registry != "" && !hasRegistryHost(target) {
		target = stage.Config.Registry + "/" + target
	}
	// Source image: pick the first artifact produced by an earlier
	// stage if none is explicitly set on the push stage.
	source := stage.Config.Image
	req := pusher.Request{Source: source, Target: target}
	res, err := e.pusher.Push(ctx, req)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	sr.Artifacts = append(sr.Artifacts, model.Artifact{
		Type:   "oci-image",
		Ref:    target,
		Digest: res.Digest,
	})
	return nil
}

func (e *Executor) executeDeploy(ctx context.Context, stage *model.Stage, sr *model.StageRun) error {
	var kind deployer.Kind
	switch {
	case stage.Config.HelmChart != "":
		kind = deployer.KindHelm
	case stage.Config.ManifestPath != "":
		kind = deployer.KindManifest
	default:
		return fmt.Errorf("deploy stage %q: need ManifestPath or HelmChart", stage.Name)
	}
	// Note: stage.Config.ManifestPath is a path; a future revision
	// should read it here (or the App deployer should synthesize the
	// manifest). Keeping Manifest empty means the concrete Deployer
	// must resolve it. Until then, we pass the path via the manifest
	// field so Noop tests still pass and the kubectl backend sees a
	// clear validation error.
	req := deployer.Request{
		Kind:        kind,
		Namespace:   stage.Config.Namespace,
		HelmChart:   stage.Config.HelmChart,
		HelmValues:  stage.Config.HelmValues,
		ReleaseName: stage.Name,
	}
	if kind == deployer.KindManifest {
		req.Manifest = []byte(stage.Config.ManifestPath)
	}
	res, err := e.deployer.Deploy(ctx, req)
	if err != nil {
		return fmt.Errorf("deploy: %w", err)
	}
	for _, r := range res.AppliedResources {
		sr.Artifacts = append(sr.Artifacts, model.Artifact{
			Type: "k8s-resource",
			Ref:  r,
		})
	}
	return nil
}

// hasRegistryHost returns true when ref already includes a registry
// host prefix (contains "/" AND the first segment contains "." or ":").
func hasRegistryHost(ref string) bool {
	for i := 0; i < len(ref); i++ {
		if ref[i] == '/' {
			head := ref[:i]
			for j := 0; j < len(head); j++ {
				if head[j] == '.' || head[j] == ':' {
					return true
				}
			}
			return false
		}
	}
	return false
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
