package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/santapong/cooker/internal/builder"
	"github.com/santapong/cooker/internal/buildplan"
	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/gitops"
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/observability"
	"github.com/santapong/cooker/internal/pusher"
	"github.com/santapong/cooker/internal/retry"
	"github.com/santapong/cooker/internal/runstate"
	"github.com/santapong/cooker/internal/stagerunner"
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

// defaultApprovalPoll is how often a blocked approval stage re-checks its
// persisted gate for an approve/reject decision. The gate is also bounded
// by the stage timeout and the run deadline (both delivered via ctx), so a
// gate that is never actioned fails the stage rather than hanging forever.
// 3s keeps the UI feeling responsive without hammering the store.
const defaultApprovalPoll = 3 * time.Second

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

// outputsEnabled reports whether inter-stage outputs (Primitive #3,
// dag-adaptation-2026.md §7.3) are active. Defaults to true; only the
// explicit values "false" / "0" disable it. Mirrors how dagMaxParallel
// reads its env var. The flag short-circuits BOTH the interpolation pass
// (stage config never sees ${stages.*} substitution) AND the ingestion
// pass (StageRun.Outputs is never populated), matching the §7.3
// "Risk + rollback" rollback knob.
func outputsEnabled() bool {
	switch os.Getenv("COOKER_OUTPUTS_ENABLED") {
	case "false", "0":
		return false
	default:
		return true
	}
}

// edgeConditionsEnabled gates edge-condition evaluation (Primitive
// #2): the per-edge success/failure/always gate, the skipped stage
// status, and the continue-through-failure runner mode. Default on;
// COOKER_EDGE_CONDITIONS_ENABLED=false restores the legacy behaviour
// (all edges treated as success-edges, first failure aborts the run
// at its level barrier). Rollback knob, same shape as outputsEnabled.
func edgeConditionsEnabled() bool {
	switch os.Getenv("COOKER_EDGE_CONDITIONS_ENABLED") {
	case "false", "0":
		return false
	default:
		return true
	}
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
	builder  builder.Builder
	pusher   pusher.Pusher
	deployer deployer.Deployer
	// dockerDeployer and composeDeployer back per-service deploy stages
	// whose DeployRuntime is "docker"/"compose" (Docker-host targets).
	// Nil falls back to the default deployer, so non-Docker installs are
	// unaffected.
	dockerDeployer  deployer.Deployer
	composeDeployer deployer.Deployer
	gitops          gitops.Writer
	// stageRunner runs Test/Custom stages in an isolated container
	// (Kubernetes Job / docker run / noop). Defaults to Noop so Execute is
	// safe to call in tests and dev without a container runtime.
	stageRunner stagerunner.Runner
	// stageApprovals persists and resolves approval-gate stages
	// (StageTypeApproval). Nil disables the gate: an approval stage then
	// fails loudly (the pre-HS26-05-03 fail-loud stub behaviour) rather
	// than auto-passing, so a pipeline can't silently skip a human gate.
	stageApprovals  *StageApprovalService
	runUpdater      RunUpdater
	logBroadcast    LogBroadcaster
	logStore        logstore.Store
	statusBroadcast StatusBroadcaster
	govHook         DeployGovernanceHook
	// approvalPoll governs how often a blocked approval stage re-checks its
	// gate. Zero uses defaultApprovalPoll. Tests set it small.
	approvalPoll time.Duration
}

// StatusBroadcaster publishes a per-stage status transition to a
// channel so the canvas can tint nodes live. The payload is the JSON
// `{"nodeId":"<stageID>","status":"<status>"}` the frontend
// usePipelineExecution hook consumes. The executor publishes on the
// run channel ("pipeline-run:<runID>"); callers wire this to the WS
// hub. Nil disables live status (logs still stream independently).
type StatusBroadcaster func(channel string, data []byte)

// DeployGovernanceHook is the executor's pre-stage admission check for
// StageTypeDeploy. Called once per deploy stage. The implementation captures
// the (service, env) target from the stage + run, builds a pre-resolved
// actor from the run's StartedBy* fields, calls Grovernance, and returns
// nil on allow OR advisory-deny. A non-nil return aborts the stage with
// the error wrapped into the stage's error column.
//
// The hook is optional. Nil means "no executor-level governance" — the
// HTTP middleware at the /apps/:id/deploy entrypoint still gates that path,
// but pipeline-defined deploys proceed without the gate. That's the
// pre-Milestone-C behaviour and remains valid for installs that haven't
// configured COOKER_GOVERNANCE_DELEGATE_TOKEN.
type DeployGovernanceHook func(ctx context.Context, run *model.PipelineRun, stage *model.Stage) error

// NewExecutor creates a pipeline executor. Without options it uses
// Noop backends so Execute is safe to call in tests and dry runs.
func NewExecutor(opts ...Option) *Executor {
	e := &Executor{
		builder:     builder.Noop{},
		pusher:      pusher.Noop{},
		deployer:    deployer.Noop{},
		gitops:      gitops.Noop{},
		stageRunner: stagerunner.Noop{},
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// broadcastStatus publishes a {nodeId,status} frame on the run's
// status channel so the canvas tints the node live. No-op when no
// broadcaster is wired or the IDs are empty.
func (e *Executor) broadcastStatus(runID, stageID, status string) {
	if e.statusBroadcast == nil || runID == "" || stageID == "" {
		return
	}
	if data := encodeStatusUpdate(stageID, status); data != nil {
		e.statusBroadcast(RunStatusChannel(runID), data)
	}
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

	// Inter-stage outputs (Primitive #3): precompute each stage's ancestor
	// set once. The interpolation resolver reads outputs ONLY from a stage's
	// ancestors — never from a concurrent sibling at the same DAG level,
	// which would be a data race on that sibling's StageRun (see
	// collectStageOutputs / TestExecutor_Outputs_ParallelSiblingsNoRace).
	// Reuses ancestorsOf, shared with the save-time validator (pipeline.go).
	outDeps := make(map[string][]string, len(p.Edges))
	for _, edge := range p.Edges {
		outDeps[edge.Target] = append(outDeps[edge.Target], edge.Source)
	}
	ancestorSets := make(map[string]map[string]bool, len(p.Stages))
	for i := range p.Stages {
		ancestorSets[p.Stages[i].ID] = ancestorsOf(p.Stages[i].ID, outDeps)
	}

	// Edge conditions (Primitive #2): each stage's incoming edges with
	// their conditions, evaluated at taskFunc entry against upstream
	// terminal statuses.
	incomingEdges := make(map[string][]model.Edge, len(p.Edges))
	for _, edge := range p.Edges {
		incomingEdges[edge.Target] = append(incomingEdges[edge.Target], edge)
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

	// Continue-through-failure is required for edge conditions: a
	// "failure"-edge downstream must still execute after its upstream
	// fails, so the runner can't abort at the level barrier. The flag
	// restores the legacy abort-on-first-error runner.
	newRunnerFn := dagrunner.NewRunnerBounded
	if edgeConditionsEnabled() {
		newRunnerFn = dagrunner.NewRunnerBoundedContinue
	}
	runner := newRunnerFn(dag, func(ctx context.Context, nodeID string) error {
		stage, ok := stageMap[nodeID]
		if !ok || stage == nil {
			return fmt.Errorf("stage %q not found in pipeline", nodeID)
		}
		stageRun, ok := stageRunMap[nodeID]
		if !ok || stageRun == nil {
			return fmt.Errorf("stage run %q not allocated", nodeID)
		}

		// Edge-condition gate (Primitive #2): evaluate every incoming
		// edge against its upstream's terminal status BEFORE any work.
		// Reading upstream StageRuns here is race-free for the same
		// reason collectStageOutputs is: the level barrier guarantees
		// upstreams finished before this taskFunc starts. A "don't run"
		// verdict stamps the terminal Skipped status and returns the
		// runner's sentinel so the rest of the level proceeds.
		if edgeConditionsEnabled() {
			statusOf := func(id string) model.RunStatus {
				if sr, ok := stageRunMap[id]; ok && sr != nil {
					return sr.Status
				}
				return model.RunStatusFailed
			}
			if !buildplan.StageShouldRun(nodeID, incomingEdges[nodeID], statusOf) {
				skippedAt := time.Now()
				if next, terr := runstate.TransitionStage(stageRun.Status, runstate.EventSkip); terr == nil {
					stageRun.Status = next
				} else {
					slog.Warn("pipeline: unexpected stage state at skip",
						"run", run.ID, "stage", stage.Name, "status", stageRun.Status, "err", terr)
					stageRun.Status = model.RunStatusSkipped
				}
				stageRun.FinishedAt = &skippedAt
				e.broadcastStatus(run.ID, stage.ID, string(stageRun.Status))
				logger.Info("pipeline stage skipped by edge conditions", "stage", stage.Name)
				return dagrunner.ErrSkipped
			}
		}

		// Inter-stage outputs (Primitive #3, dag-adaptation-2026.md §7.3).
		// Resolve ${stages.<id>.<key>} references in this stage's config
		// against completed upstream outputs BEFORE dispatch. The result is
		// a copy (execStage) — the shared stage/stageMap is never mutated.
		// A resolution failure (unknown stage/key) is a deterministic stage
		// failure that must NOT be retried: ierrInterp carries it past the
		// retry.Do block into the existing terminal failure handler below.
		execStage := stage
		var ierrInterp error
		if outputsEnabled() {
			resolver := buildplan.OutputResolver{Stages: collectStageOutputs(stageRunMap, ancestorSets[nodeID])}
			cp, ierr := interpolateStageConfig(stage, resolver)
			if ierr != nil {
				ierrInterp = ierr
			} else {
				execStage = cp
			}
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
		e.broadcastStatus(run.ID, stage.ID, string(stageRun.Status))

		logger.Info("pipeline executing stage",
			"stage", stage.Name, "type", stage.Type, "timeout", timeout)

		// Wrap the type-specific dispatch with retry so transient
		// adapter errors (registry 5xx, kube-API blip) don't fail
		// the whole pipeline. Approval / custom stages don't make
		// sense to retry — policyFromStage pins them to one attempt.
		retryPolicy := policyFromStage(stage)

		// Config interpolation failure (above) is deterministic: bypass
		// retry.Do entirely and route ierrInterp through the same terminal
		// failure-handling block below. Otherwise dispatch the (possibly
		// interpolated) execStage — but always pass the real stageRun
		// pointer so artifacts/outputs/logs land on the persisted row.
		var stageErr error
		if ierrInterp != nil {
			stageErr = ierrInterp
		} else {
			stageErr = retry.Do(stageCtx, retryPolicy, func(ctx context.Context) error {
				switch execStage.Type {
				case model.StageTypeBuild:
					return e.executeBuild(ctx, run.ID, execStage, stageRun)
				case model.StageTypeTest:
					return e.executeTest(ctx, run.ID, execStage, stageRun)
				case model.StageTypePush:
					return e.executePush(ctx, run.ID, execStage, stageRun)
				case model.StageTypeDeploy:
					// Pre-stage governance check (Milestone C). Runs once
					// per stage attempt — retries don't re-prompt the gate.
					// On enforce-deny the stage fails immediately with the
					// policy reason. On advisory-deny the hook returns nil
					// and slog-logs the would-have-blocked event itself.
					if e.govHook != nil {
						if err := e.govHook(ctx, run, execStage); err != nil {
							return err
						}
					}
					return e.executeDeploy(ctx, run.ID, execStage, stageRun)
				case model.StageTypeApproval:
					return e.executeApproval(ctx, run.ID, execStage, stageRun)
				case model.StageTypeCustom:
					return e.executeCustom(ctx, run.ID, execStage, stageRun)
				case model.StageTypeGitOpsCommit:
					return e.executeGitOpsCommit(ctx, execStage, stageRun)
				default:
					return fmt.Errorf("unknown stage type: %s", execStage.Type)
				}
			})
		}

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
			e.broadcastStatus(run.ID, stage.ID, string(stageRun.Status))
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
		e.broadcastStatus(run.ID, stage.ID, string(stageRun.Status))
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
				if update.Status == "failed" || update.Status == "success" || update.Status == "skipped" {
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

// newStageLineWriter returns the per-line tee for a stage's adapter
// output, or nil when neither live broadcast nor replay storage is
// configured (in which case the caller writes only to the capped on-disk
// buffer). The lineWriter stamps seq+ts, appends to the log store for
// replay-on-connect, AND broadcasts the wire envelope — either leg may be
// nil. Production callers always pass non-empty runID + stage.ID; the
// guard keeps direct test callers (which may omit them) safe (W10-7).
func (e *Executor) newStageLineWriter(runID string, stage *model.Stage) *lineWriter {
	if e.logBroadcast == nil && e.logStore == nil {
		return nil
	}
	if runID == "" || stage == nil || stage.ID == "" {
		return nil
	}
	return newLineWriter(e.logBroadcast, e.logStore, runID, stage.ID)
}

