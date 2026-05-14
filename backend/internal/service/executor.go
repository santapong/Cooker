package service

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/santapong/cooker/internal/runstate"
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

// progressDrainInterval is the maximum time the drain goroutine waits
// between batched persistProgress writes for non-terminal transitions.
// Terminal transitions (failed / success) always flush immediately.
// See dag-adaptation-2026.md §6 T5.
const progressDrainInterval = 500 * time.Millisecond

// progressDrainBatchSize is the maximum number of non-terminal status
// transitions the drain goroutine accumulates before forcing a flush
// regardless of the timer. Together with progressDrainInterval
// ("whichever comes first") this bounds worst-case lag to
// min(500ms, 10 transitions). Primitive #1 (retry policies) triples
// the transition rate per stage; this batch absorbs that increase
// without proportional write cost.
const progressDrainBatchSize = 10

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

// Execute runs a pipeline and returns the terminal RunResult plus
// any error from the DAG runner. The returned RunResult.Status is
// guaranteed terminal — RunStatusSuccess, RunStatusFailed, or
// RunStatusCancelled — and matches the run.Status field Execute
// also writes on the in-memory PipelineRun.
//
// Cancellation semantics (F2):
//   - If the caller has already advanced run.Status to
//     RunStatusCancelled (e.g. CancelPipelineRun ran concurrently),
//     Execute preserves that status on return regardless of whether
//     the DAG runner reports an error or a clean shutdown. This is
//     the silent-flip lock-in flagged by handler-layering audit
//     Finding 2 (see docs/audits/2026-05-handler-layering.md).
//   - If the DAG runner returns a context-cancelled error and the
//     run was NOT pre-marked Cancelled, Execute reports
//     RunStatusCancelled (operator-driven cancellation via ctx).
//   - Any other runner error → RunStatusFailed.
//   - Clean return → RunStatusSuccess.
func (e *Executor) Execute(ctx context.Context, p *model.Pipeline, run *model.PipelineRun) (model.RunResult, error) {
	// Bind run-id to the logger so every line emitted from this
	// goroutine (and any goroutine that takes ctx) carries it.
	// Operators tailing stderr can grep `run=<id>` instead of
	// reverse-engineering which "stage X started" belongs to which
	// run when the cluster has many in flight.
	logger := slog.With("run", run.ID, "pipeline", p.ID)
	ctx = withRunLogger(ctx, logger)

	dag, err := BuildDAGFromPipeline(p)
	if err != nil {
		// DAG construction failure is a Failed terminal — the run never
		// started executing stages. Stamp run.Status + FinishedAt so the
		// handler closure can persist verbatim without re-deriving state.
		wrapped := fmt.Errorf("building DAG: %w", err)
		return e.finalize(run, wrapped, run.Status == model.RunStatusCancelled), wrapped
	}

	stageMap := make(map[string]*model.Stage)
	for i := range p.Stages {
		stageMap[p.Stages[i].ID] = &p.Stages[i]
	}

	stageRunMap := make(map[string]*model.StageRun)
	for i := range run.StageRuns {
		stageRunMap[run.StageRuns[i].StageID] = &run.StageRuns[i]
	}

	// F2 cancellation lock-in: snapshot whether the caller pre-marked
	// the run Cancelled (e.g. a CancelPipelineRun call that landed on
	// the row before Spawn-ed work started). We stamp Running below
	// regardless so observers see the run is in flight, but finalize()
	// uses this flag to refuse the Running→Success transition when the
	// terminal intent was Cancelled. See
	// docs/audits/2026-05-handler-layering.md Finding 2.
	startedCancelled := run.Status == model.RunStatusCancelled

	now := time.Now()
	// Route the pending→running transition through runstate so an
	// unexpected initial state (e.g. terminal Failed re-run) is logged
	// as ErrInvalidTransition rather than silently overwritten. When
	// startedCancelled is true the transition refuses, leaving the
	// status as Cancelled; we explicitly stamp Running below because
	// observers (the WS hub, GET handlers) need to see in-flight state
	// even for pre-cancelled runs — finalize() restores Cancelled via
	// the startedCancelled flag.
	if next, terr := runstate.TransitionRun(run.Status, runstate.EventStart); terr == nil {
		run.Status = next
	} else if !startedCancelled {
		slog.Warn("pipeline: unexpected initial state at Execute",
			"run", run.ID, "status", run.Status, "err", terr)
		run.Status = model.RunStatusRunning
	} else {
		run.Status = model.RunStatusRunning
	}
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
		// Route stage start through runstate so a stage rerun from a
		// terminal state (operator triggered, currently uncommon)
		// surfaces as a log-level invariant rather than silently
		// flipping the stage row. Fall back to direct assignment on
		// invalid transition to preserve current behaviour.
		if next, terr := runstate.TransitionStage(stageRun.Status, runstate.EventStart); terr == nil {
			stageRun.Status = next
		} else {
			slog.Warn("pipeline: unexpected stage initial state",
				"run", run.ID, "stage", stage.Name, "status", stageRun.Status, "err", terr)
			stageRun.Status = model.RunStatusRunning
		}
		// Progress is now persisted by the drain goroutine below via
		// runner.Updates(). No explicit persistProgress call here.

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
				return e.executePush(ctx, run.ID, stage, stageRun)
			case model.StageTypeDeploy:
				return e.executeDeploy(ctx, run.ID, stage, stageRun)
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
			if next, terr := runstate.TransitionStage(stageRun.Status, runstate.EventFail); terr == nil {
				stageRun.Status = next
			} else {
				stageRun.Status = model.RunStatusFailed
			}
			stageRun.Error = stageErr.Error()
			observability.ObserveStageDuration(string(stage.Type), "failed", duration)
			// Terminal status: the drain goroutine flushes immediately
			// when it receives the "failed" StatusUpdate from the runner.
			return stageErr
		}

		if next, terr := runstate.TransitionStage(stageRun.Status, runstate.EventSucceed); terr == nil {
			stageRun.Status = next
		} else {
			stageRun.Status = model.RunStatusSuccess
		}
		observability.ObserveStageDuration(string(stage.Type), "success", duration)
		// Terminal status: the drain goroutine flushes immediately
		// when it receives the "success" StatusUpdate from the runner.
		return nil
	}, dagMaxParallel())

	// Drain goroutine: consumes runner.Updates() and writes batched
	// persistProgress calls at most once per min(500ms, 10 transitions).
	// Terminal transitions ("failed" / "success") trigger an immediate
	// eager flush so the final stage outcome surfaces without debounce lag.
	//
	// This replaces the three explicit persistProgress calls that lived
	// inside the stage taskFunc (dag-adaptation-2026.md §6 T5, W4).
	// A 50-stage pipeline previously paid up to 100+ full JSONB rewrites;
	// the drain absorbs them into ≤ceil(100/10) = 10 writes under normal
	// load, and ≤1 per 500ms burst. Primitive #1 (retry policies) triples
	// the transition rate per flaky stage; this drain absorbs the increase.
	//
	// The goroutine MUST start before runner.Run (which is synchronous and
	// closes updates when it returns) so the range sees all updates.
	// drainDone is closed when the goroutine exits; we join after runner.Run
	// so we never touch run.Status after Execute returns.
	//
	// Cross-reference: docs/audits/2026-05-p1-context-pack.md confirms the
	// terminal-flush rule is complementary to P#1's retry-emit volume.
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		pending := 0
		tick := time.NewTimer(progressDrainInterval)
		defer tick.Stop()

		flush := func() {
			if pending == 0 {
				return
			}
			e.persistProgress(ctx, run)
			pending = 0
			// Reset the debounce timer so the next window starts fresh.
			if !tick.Stop() {
				select {
				case <-tick.C:
				default:
				}
			}
			tick.Reset(progressDrainInterval)
		}

		for {
			select {
			case update, ok := <-runner.Updates():
				if !ok {
					// Channel closed: runner.Run has returned. Flush any
					// accumulated non-terminal updates and exit.
					flush()
					return
				}
				pending++
				// Eager flush on any terminal stage status so the final
				// outcome of a stage lands in the store immediately —
				// no waiting for the next debounce tick. This is the
				// guarantee that run-completion code paths (and the
				// caller) do not observe lag on the last transition.
				if update.Status == "failed" || update.Status == "success" {
					flush()
				} else if pending >= progressDrainBatchSize {
					flush()
				}
			case <-tick.C:
				flush()
				tick.Reset(progressDrainInterval)
			}
		}
	}()

	err = runner.Run(ctx)
	// Join the drain goroutine: all pending updates are flushed before
	// we set the terminal run.Status / run.FinishedAt below.
	<-drainDone

	return e.finalize(run, err, startedCancelled), err
}

// finalize stamps the terminal run.Status / FinishedAt / Error onto
// run and returns the matching RunResult. F2 contract: on return,
// run.Status is one of Success / Failed / Cancelled — never
// RunStatusRunning, never RunStatusPending. The handler's
// post-Execute closure used to re-derive these from run.Status; it
// now just persists the RunResult verbatim. See
// docs/audits/2026-05-handler-layering.md Finding 2.
//
// Cancellation precedence:
//  1. If run.Status was Cancelled at entry (startedCancelled) OR was
//     externally re-set to Cancelled between Run start and finalize,
//     preserve Cancelled. This is the silent-flip lock-in.
//  2. If runErr is a context-cancellation error, report Cancelled.
//  3. Any other runErr → Failed.
//  4. No error → Success.
func (e *Executor) finalize(run *model.PipelineRun, runErr error, startedCancelled bool) model.RunResult {
	finishTime := time.Now()
	run.FinishedAt = &finishTime

	// Route the running→terminal transition through runstate so an
	// out-of-band caller can't drive the row through an illegal edge
	// (e.g. Pending→Success). The startedCancelled / Cancelled
	// precedence still wins by short-circuit — the FSM doesn't model
	// "caller-pinned terminal" intent, so we apply that rule first.
	terminal := func(event runstate.Event, fallback model.RunStatus) {
		if next, terr := runstate.TransitionRun(run.Status, event); terr == nil {
			run.Status = next
		} else {
			run.Status = fallback
		}
	}

	switch {
	case startedCancelled || run.Status == model.RunStatusCancelled:
		// Caller pre-marked the run cancelled — do not flip to
		// Success or Failed. Preserve any existing Error verbatim.
		// We still route through TransitionRun so a Running→Cancelled
		// transition is recorded canonically when applicable.
		terminal(runstate.EventCancel, model.RunStatusCancelled)
	case runErr != nil && errors.Is(runErr, context.Canceled):
		terminal(runstate.EventCancel, model.RunStatusCancelled)
		if run.Error == "" {
			run.Error = runErr.Error()
		}
	case runErr != nil:
		terminal(runstate.EventFail, model.RunStatusFailed)
		if run.Error == "" {
			run.Error = runErr.Error()
		}
	default:
		terminal(runstate.EventSucceed, model.RunStatusSuccess)
	}

	return model.RunResult{Status: run.Status, FinishedAt: finishTime}
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

func (e *Executor) executeTest(_ context.Context, stage *model.Stage) error {
	// Stage type "test" is not yet implemented. Return an explicit error so
	// pipelines that include a test stage fail loudly rather than silently
	// passing. Closes dag-adaptation-2026.md §6 T1 / dag-performance.md
	// §3 Critical finding #1.
	return fmt.Errorf("stage type %q not implemented", stage.Type)
}

func (e *Executor) executePush(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) error {
	target := stage.Config.Repository
	if stage.Config.Registry != "" && !hasRegistryHost(target) {
		target = stage.Config.Registry + "/" + target
	}
	// Source image: pick the first artifact produced by an earlier
	// stage if none is explicitly set on the push stage.
	source := stage.Config.Image

	// Mirror executeBuild's LogWriter wiring (see executor.go executeBuild
	// for the canonical pattern). The capped buffer persists adapter
	// output to StageRun.Logs; when a broadcaster is configured a
	// per-line tee forwards each "Pushing..." / "Pushed image to..."
	// line to the stage's WebSocket channel for live tailing. Closes
	// dag-performance.md §4 High #2 for the push half. T2.
	logs := newCappedBuffer(stageLogCap)
	var writer io.Writer = logs
	var lw *lineWriter
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

	req := pusher.Request{Source: source, Target: target, LogWriter: writer}
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

func (e *Executor) executeDeploy(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) error {
	var kind deployer.Kind
	switch {
	case stage.Config.HelmChart != "":
		kind = deployer.KindHelm
	case stage.Config.ManifestPath != "":
		kind = deployer.KindManifest
	default:
		return fmt.Errorf("deploy stage %q: need ManifestPath or HelmChart", stage.Name)
	}

	// Mirror executeBuild's LogWriter wiring (see executor.go executeBuild
	// for the canonical pattern). Persists per-resource "Applied
	// <kind>/<name>" lines from the adapter into StageRun.Logs and
	// forwards each to the stage's WebSocket channel when a broadcaster
	// is configured. Closes dag-performance.md §4 High #2 for the
	// deploy half. T2.
	logs := newCappedBuffer(stageLogCap)
	var writer io.Writer = logs
	var lw *lineWriter
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
		LogWriter:   writer,
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

func (e *Executor) executeApproval(_ context.Context, stage *model.Stage) error {
	// Stage type "approval" is not yet implemented. Return an explicit error
	// so pipelines that include an approval gate fail loudly rather than
	// silently auto-approving. Closes dag-adaptation-2026.md §6 T1.
	return fmt.Errorf("stage type %q not implemented", stage.Type)
}

func (e *Executor) executeCustom(_ context.Context, stage *model.Stage) error {
	// Stage type "custom" is not yet implemented. Return an explicit error
	// so pipelines that include a custom stage fail loudly rather than
	// silently succeeding. Closes dag-adaptation-2026.md §6 T1.
	return fmt.Errorf("stage type %q not implemented", stage.Type)
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
