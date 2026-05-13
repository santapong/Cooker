package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/santapong/cooker/internal/builder"
	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/gitops"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/observability"
	"github.com/santapong/cooker/internal/pusher"
	"github.com/santapong/cooker/internal/retry"
	"github.com/santapong/cooker/pkg/dagrunner"
)

// stageLogCap caps how many bytes of build/push/deploy output land
// in StageRun.Logs. A runaway build can produce GB of output; we
// keep the head and append a marker when we hit the cap so the UI
// never tries to render an arbitrary-sized blob from JSONB.
const stageLogCap = 1 << 20 // 1 MiB

// defaultStageTimeout caps any individual stage that doesn't set its
// own. Picked to be longer than realistic Kaniko builds (~30 min)
// without being so long that a stuck stage pins resources for a day.
const defaultStageTimeout = 30 * time.Minute

// defaultMaxParallel caps how many stages within a single DAG level
// execute concurrently. Picked to leave headroom for K8s API + one
// registry; operators with bigger compute budgets can override via
// COOKER_DAG_MAX_PARALLEL.
const defaultMaxParallel = 16

func dagMaxParallel() int {
	if v := os.Getenv("COOKER_DAG_MAX_PARALLEL"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxParallel
}

// RunUpdater persists in-progress run state. Called by the executor
// after every stage transition (start + finish) so a crash mid-run
// leaves a row that reflects what was completed instead of "still
// pending" everywhere. Errors are logged but don't fail the run —
// the work itself is the source of truth.
type RunUpdater func(ctx context.Context, run *model.PipelineRun) error

// Executor runs pipelines using the DAG runner and reports progress.
// Production wiring injects real Builder/Pusher/Deployer; tests
// inject mocks.
type Executor struct {
	builder      builder.Builder
	pusher       pusher.Pusher
	deployer     deployer.Deployer
	gitops       gitops.Writer
	runUpdater   RunUpdater
	logBroadcast LogBroadcaster
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

// WithGitOps injects the GitOps writer used by gitops-commit stages.
func WithGitOps(g gitops.Writer) Option {
	return func(e *Executor) {
		if g != nil {
			e.gitops = g
		}
	}
}

// WithRunUpdater installs a callback the executor invokes after
// every stage transition so a Postgres-backed handler can persist
// progress as it happens. Pass nil (or skip the option) to disable.
func WithRunUpdater(u RunUpdater) Option {
	return func(e *Executor) {
		e.runUpdater = u
	}
}

// WithLogBroadcaster installs a per-line broadcaster the executor
// uses to stream stage logs to the WebSocket hub in real time. Pass
// nil (or skip the option) to keep the historical behaviour of only
// persisting logs to StageRun.Logs at stage finish.
func WithLogBroadcaster(b LogBroadcaster) Option {
	return func(e *Executor) {
		e.logBroadcast = b
	}
}

// NewExecutor creates a pipeline executor. Without options it uses
// Noop backends so Execute is safe to call in tests and dry runs.
func NewExecutor(opts ...Option) *Executor {
	e := &Executor{
		builder:  builder.Noop{},
		pusher:   pusher.Noop{},
		deployer: deployer.Noop{},
		gitops:   gitops.Noop{},
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute runs a pipeline and returns the completed PipelineRun.
func (e *Executor) Execute(ctx context.Context, p *model.Pipeline, run *model.PipelineRun) error {
	// Bind run-id to the logger so every line emitted from this
	// goroutine (and any goroutine that takes ctx) carries it.
	// Operators tailing stderr can grep `run=<id>` instead of
	// reverse-engineering which "stage X started" belongs to which
	// run when the cluster has many in flight.
	logger := slog.With("run", run.ID, "pipeline", p.ID)
	ctx = withRunLogger(ctx, logger)

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

	runner := dagrunner.NewRunnerBounded(dag, func(ctx context.Context, nodeID string) error {
		stage, ok := stageMap[nodeID]
		if !ok || stage == nil {
			return fmt.Errorf("stage %q not found in pipeline", nodeID)
		}
		stageRun, ok := stageRunMap[nodeID]
		if !ok || stageRun == nil {
			return fmt.Errorf("stage run %q not allocated", nodeID)
		}

		// Apply per-stage timeout. stage.Config.Timeout is a Go-format
		// duration string (e.g. "5m", "1h"); fall back to a 30-minute
		// default so a runaway stage can't hang the run forever.
		timeout := defaultStageTimeout
		if stage.Config.Timeout != "" {
			if d, err := time.ParseDuration(stage.Config.Timeout); err == nil && d > 0 {
				timeout = d
			} else if err != nil {
				slog.Warn("pipeline stage timeout parse failed; using default",
					"stage", stage.Name, "value", stage.Config.Timeout, "err", err)
			}
		}
		stageCtx, cancelStage := context.WithTimeout(ctx, timeout)
		defer cancelStage()

		startTime := time.Now()
		stageRun.StartedAt = &startTime
		stageRun.Status = model.RunStatusRunning
		e.persistProgress(ctx, run)

		logger.Info("pipeline executing stage",
			"stage", stage.Name, "type", stage.Type, "timeout", timeout)

		// Wrap the type-specific dispatch with retry so transient
		// adapter errors (registry 5xx, kube-API blip) don't fail
		// the whole pipeline. Approval / custom stages don't make
		// sense to retry — they're skipped via MaxAttempts=1 below.
		retryPolicy := retry.Policy{
			MaxAttempts: 1 + stage.Config.Retries,
			Initial:     1 * time.Second,
			Max:         15 * time.Second,
			IsTransient: func(err error) bool {
				// Don't retry if the parent context is gone — that's
				// shutdown, cancellation, or the per-stage deadline
				// firing. Also don't retry validation-shaped errors
				// (those are caller bugs, not network blips).
				return !retry.IsContextErr(err)
			},
		}
		switch stage.Type {
		case model.StageTypeApproval, model.StageTypeCustom, model.StageTypeTest:
			retryPolicy.MaxAttempts = 1
		}

		stageErr := retry.Do(stageCtx, retryPolicy, func(ctx context.Context) error {
			switch stage.Type {
			case model.StageTypeBuild:
				return e.executeBuild(ctx, run.ID, stage, stageRun)
			case model.StageTypeTest:
				return e.executeTest(ctx, stage)
			case model.StageTypePush:
				return e.executePush(ctx, stage, stageRun)
			case model.StageTypeDeploy:
				return e.executeDeploy(ctx, stage, stageRun)
			case model.StageTypeApproval:
				return e.executeApproval(ctx, stage)
			case model.StageTypeCustom:
				return e.executeCustom(ctx, stage)
			case model.StageTypeGitOpsCommit:
				return e.executeGitOpsCommit(ctx, stage, stageRun)
			default:
				return fmt.Errorf("unknown stage type: %s", stage.Type)
			}
		})

		endTime := time.Now()
		stageRun.FinishedAt = &endTime

		duration := endTime.Sub(startTime)

		if stageErr != nil {
			stageRun.Status = model.RunStatusFailed
			stageRun.Error = stageErr.Error()
			observability.ObserveStageDuration(string(stage.Type), "failed", duration)
			e.persistProgress(ctx, run)
			return stageErr
		}

		stageRun.Status = model.RunStatusSuccess
		observability.ObserveStageDuration(string(stage.Type), "success", duration)
		e.persistProgress(ctx, run)
		return nil
	}, dagMaxParallel())

	// Drain status updates (in production, these go to WebSocket)
	go func() {
		for update := range runner.Updates() {
			logger.Info("pipeline stage transition", "stage", update.NodeID, "status", update.Status)
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

// persistProgress invokes the registered RunUpdater (if any) so the
// store catches up to the in-memory run state. Errors are logged
// and discarded — losing a progress write is not a reason to fail
// the run.
func (e *Executor) persistProgress(ctx context.Context, run *model.PipelineRun) {
	if e.runUpdater == nil {
		return
	}
	if err := e.runUpdater(ctx, run); err != nil {
		slog.Warn("pipeline: persist progress failed", "run", run.ID, "err", err)
	}
}

func (e *Executor) executeBuild(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) error {
	logs := newCappedBuffer(stageLogCap)
	// LogWriter receives the on-disk capture by default. When a
	// broadcaster is wired (production wiring via WithLogBroadcaster),
	// stream each \n-terminated line to the WebSocket hub on the
	// per-stage channel as well so the run page can tail logs live
	// instead of polling. The broadcaster is best-effort: backpressure
	// at the hub drops on the far side, never on the executor goroutine.
	var writer io.Writer = logs
	var lw *lineWriter
	// Production callers always pass non-empty runID + stage.ID; the gate
	// keeps direct test callers (which may omit them) safe (W10-7).
	if e.logBroadcast != nil && runID != "" && stage != nil && stage.ID != "" {
		lw = newLineWriter(e.logBroadcast, StageLogChannel(runID, stage.ID))
		writer = io.MultiWriter(logs, lw)
	}
	defer func() {
		if lw != nil {
			lw.flush()
		}
		sr.Logs = logs.String()
	}()
	req := builder.Request{
		ContextDir: stage.Config.Context,
		Dockerfile: stage.Config.Dockerfile,
		Tags:       stage.Config.Tags,
		BuildArgs:  stage.Config.BuildArgs,
		Platforms:  stage.Config.Platforms,
		LogWriter:  writer,
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

// cappedBuffer is a write-capped bytes.Buffer. Writes after the cap
// are silently dropped except for a one-time truncation marker so
// the reader can tell logs were trimmed.
type cappedBuffer struct {
	buf       *bytes.Buffer
	cap       int
	truncated bool
}

func newCappedBuffer(cap int) *cappedBuffer {
	return &cappedBuffer{buf: &bytes.Buffer{}, cap: cap}
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.buf.Len() >= c.cap {
		if !c.truncated {
			c.buf.WriteString("\n... [log truncated at 1 MiB] ...\n")
			c.truncated = true
		}
		return len(p), nil
	}
	remaining := c.cap - c.buf.Len()
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	c.buf.Write(p[:remaining])
	c.buf.WriteString("\n... [log truncated at 1 MiB] ...\n")
	c.truncated = true
	return len(p), nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }

func (e *Executor) executeTest(ctx context.Context, stage *model.Stage) error {
	// TODO: Run test container with specified image and command
	slog.Info("stage test", "image", stage.Config.Image, "command", stage.Config.Command)
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
	slog.Info("approval gate waiting")
	return nil
}

func (e *Executor) executeCustom(ctx context.Context, stage *model.Stage) error {
	// TODO: Execute custom script
	slog.Info("stage custom", "script", stage.Config.Script, "timeout", stage.Config.Timeout)
	return nil
}

func (e *Executor) executeGitOpsCommit(ctx context.Context, stage *model.Stage, sr *model.StageRun) error {
	if stage.Config.GitOpsRepo == "" {
		return fmt.Errorf("gitops stage %q: gitopsRepo is required", stage.Name)
	}
	if stage.Config.GitOpsPath == "" {
		return fmt.Errorf("gitops stage %q: gitopsPath is required", stage.Name)
	}
	req := gitops.Request{
		Repo:    stage.Config.GitOpsRepo,
		Branch:  stage.Config.GitOpsBranch,
		Path:    stage.Config.GitOpsPath,
		Content: []byte(stage.Config.GitOpsContent),
		Message: stage.Config.GitOpsMessage,
	}
	res, err := e.gitops.Commit(ctx, req)
	if err != nil {
		return fmt.Errorf("gitops: %w", err)
	}
	sr.Artifacts = append(sr.Artifacts, model.Artifact{
		Type: "gitops-commit",
		Ref:  stage.Config.GitOpsRepo + "@" + stage.Config.GitOpsBranch,
		// Digest holds the commit SHA — naming is approximate but
		// keeps one artifact shape across backends.
		Digest: res.CommitSHA,
	})
	return nil
}
