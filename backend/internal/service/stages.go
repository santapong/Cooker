package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/gitops"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/build/pusher"
	"github.com/santapong/cooker/internal/retry"
	"github.com/santapong/cooker/internal/build/stagerunner"
)

func (e *Executor) executeBuild(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) error {
	logs := newCappedBuffer(stageLogCap)
	// LogWriter receives the on-disk capture by default. When a
	// broadcaster is wired (production wiring via WithLogBroadcaster),
	// stream each \n-terminated line to the WebSocket hub on the
	// per-stage channel as well so the run page can tail logs live
	// instead of polling. The broadcaster is best-effort: backpressure
	// at the hub drops on the far side, never on the executor goroutine.
	var writer io.Writer = logs
	lw := e.newStageLineWriter(runID, stage)
	if lw != nil {
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
		Cache:      builderCacheSpec(stage.Config.Cache),
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
	// Inter-stage outputs (Primitive #3): expose digest/tag/tags for
	// downstream ${stages.<id>.<key>} references. Adapter-emitted outputs
	// (res.Outputs) overlay the derived baseline. Gated by the same flag
	// as the interpolation pass so the rollback knob disables both halves.
	if outputsEnabled() {
		applyStageOutputs(sr, deriveBuildOutputs(res), res.Outputs)
	}
	return nil
}

// policyFromStage builds the retry.Policy for one stage. The
// structured StageConfig.Retry wins over the legacy Retries int; all
// knobs are clamped (MaxAttempts [1,10], Initial [100ms,60s], Max
// [Initial,5m]) so a typo'd policy can't spin a stage for hours.
// Exponential=false pins every delay to Initial (Max=Initial does
// that without touching internal/retry) — applied after the clamps so
// an out-of-range InitialMS can't leave Max above Initial and
// re-enable growth. Approval / custom / test stages never retry.
func policyFromStage(stage *model.Stage) retry.Policy {
	p := retry.Policy{
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
	r := stage.Config.Retry
	if r != nil {
		if r.MaxAttempts > 0 {
			p.MaxAttempts = r.MaxAttempts
		}
		if r.InitialMS > 0 {
			p.Initial = time.Duration(r.InitialMS) * time.Millisecond
		}
		if r.MaxMS > 0 {
			p.Max = time.Duration(r.MaxMS) * time.Millisecond
		}
	}
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	if p.MaxAttempts > 10 {
		p.MaxAttempts = 10
	}
	if p.Initial < 100*time.Millisecond {
		p.Initial = 100 * time.Millisecond
	}
	if p.Initial > time.Minute {
		p.Initial = time.Minute
	}
	if p.Max < p.Initial {
		p.Max = p.Initial
	}
	if p.Max > 5*time.Minute {
		p.Max = 5 * time.Minute
	}
	if r != nil && r.Exponential != nil && !*r.Exponential {
		p.Max = p.Initial
	}
	switch stage.Type {
	case model.StageTypeApproval, model.StageTypeCustom, model.StageTypeTest:
		p.MaxAttempts = 1
	}
	return p
}

// builderCacheSpec maps the stage's cache config onto the builder
// package's mirror type. The save-time validator already rejected
// malformed refs; a nil spec maps to the zero value (cache off).
func builderCacheSpec(c *model.CacheSpec) builder.CacheSpec {
	if c == nil {
		return builder.CacheSpec{}
	}
	return builder.CacheSpec{Mode: c.Mode, Ref: c.Ref, Inline: c.Inline}
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

// executeTest runs a Test stage: a user-specified image + command in an
// isolated container, pass/fail by exit code. Thin wrapper over the shared
// container runner. Closes HS26-05-03 (test runner) — formerly a fail-loud
// stub. The UI collects only the test image today (NodeConfigPanel
// `config.image`); Command is honoured if a hand-built pipeline sets it.
func (e *Executor) executeTest(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) error {
	if stage.Config.Image == "" {
		return fmt.Errorf("test stage %q: image is required", stage.Name)
	}
	return e.runStageContainer(ctx, runID, stage, sr, stagerunner.Request{
		Image:   stage.Config.Image,
		Command: stage.Config.Command,
		Env:     stage.Config.Env,
	})
}

// runStageContainer is the shared Test/Custom execution path: it wires the
// stage-log tee (same pattern as executeBuild), invokes the configured
// stage runner, persists logs, and maps the container exit code onto the
// stage outcome. A non-zero exit fails the stage; a runtime error
// (ErrUnavailable, lost API) also fails it but with the wrapped cause.
func (e *Executor) runStageContainer(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun, req stagerunner.Request) error {
	logs := newCappedBuffer(stageLogCap)
	var writer io.Writer = logs
	lw := e.newStageLineWriter(runID, stage)
	if lw != nil {
		writer = io.MultiWriter(logs, lw)
	}
	defer func() {
		if lw != nil {
			lw.flush()
		}
		sr.Logs = logs.String()
	}()
	req.LogWriter = writer

	runner := e.stageRunner
	if runner == nil {
		runner = stagerunner.Noop{}
	}
	res, err := runner.Run(ctx, req)
	if err != nil {
		return fmt.Errorf("%s: %w", stage.Type, err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s stage %q: command exited %d", stage.Type, stage.Name, res.ExitCode)
	}
	return nil
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
	lw := e.newStageLineWriter(runID, stage)
	if lw != nil {
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
	// Inter-stage outputs (Primitive #3): expose digest/ref. target is the
	// resolved destination ref (already interpolated upstream when it
	// contained a ${stages.*} token). Adapter outputs overlay the baseline.
	if outputsEnabled() {
		applyStageOutputs(sr, derivePushOutputs(res, target), res.Outputs)
	}
	return nil
}

func (e *Executor) executeDeploy(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) error {
	// Select the deployer + request kind. DeployRuntime (set by compose
	// per-service synthesis) routes docker/compose stages to the Docker
	// deployers; everything else keeps the legacy manifest/helm dispatch
	// on the default deployer.
	dep := e.deployer
	var kind deployer.Kind
	switch stage.Config.DeployRuntime {
	case "docker":
		kind = deployer.KindDockerRun
		if e.dockerDeployer != nil {
			dep = e.dockerDeployer
		}
	case "compose":
		kind = deployer.KindCompose
		if e.composeDeployer != nil {
			dep = e.composeDeployer
		}
	default:
		switch {
		case stage.Config.HelmChart != "":
			kind = deployer.KindHelm
		case stage.Config.ManifestPath != "":
			kind = deployer.KindManifest
		default:
			return fmt.Errorf("deploy stage %q: need ManifestPath or HelmChart", stage.Name)
		}
	}

	// Mirror executeBuild's LogWriter wiring (see executor.go executeBuild
	// for the canonical pattern). Persists per-resource "Applied
	// <kind>/<name>" lines from the adapter into StageRun.Logs and
	// forwards each to the stage's WebSocket channel when a broadcaster
	// is configured. Closes dag-performance.md §4 High #2 for the
	// deploy half. T2.
	logs := newCappedBuffer(stageLogCap)
	var writer io.Writer = logs
	lw := e.newStageLineWriter(runID, stage)
	if lw != nil {
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
		Image:       stage.Config.Image,
		LogWriter:   writer,
	}
	switch kind {
	case deployer.KindManifest:
		req.Manifest = []byte(stage.Config.ManifestPath)
	case deployer.KindDockerRun:
		// Per-service docker run: name from the compose service, with
		// resource limits and the build/image tag from the stage.
		req.Name = stage.Config.ComposeServiceName
		req.Ports = composePortsToPublish(stage.Config)
		req.Env = stage.Config.Env
		if r := stage.Config.Resources; r != nil {
			req.Resources = &deployer.ResourceLimits{Memory: r.Memory, CPUs: r.CPUs}
		}
	case deployer.KindCompose:
		req.Name = stage.Config.ComposeServiceName
		req.ComposeFile = stage.Config.ManifestPath // reused field: compose-file path
	}
	res, err := dep.Deploy(ctx, req)
	if err != nil {
		return fmt.Errorf("deploy: %w", err)
	}
	resType := "k8s-resource"
	if kind == deployer.KindDockerRun || kind == deployer.KindCompose {
		resType = "container"
	}
	for _, r := range res.AppliedResources {
		sr.Artifacts = append(sr.Artifacts, model.Artifact{
			Type: resType,
			Ref:  r,
		})
	}
	// Inter-stage outputs (Primitive #3): expose the applied resources as a
	// comma-joined list. Adapter outputs overlay the derived baseline.
	if outputsEnabled() {
		applyStageOutputs(sr, deriveDeployOutputs(res), res.Outputs)
	}
	return nil
}

// composePortsToPublish returns the port specs a docker-run deploy
// should publish, carried from the compose service's `ports:` during
// synthesis (StageConfig.ComposePorts). The docker deployer normalizes
// a bare "80" to "80:80". (K8s deploys publish via the synthesized
// manifest's port instead.)
func composePortsToPublish(cfg model.StageConfig) []string {
	return cfg.ComposePorts
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

// executeApproval is a persisted human-in-the-loop pause gate. It opens
// (or re-attaches to) a StageApproval row, broadcasts the stage as
// "awaiting" so the run page surfaces an Approve/Reject affordance, then
// blocks — polling the gate — until it is approved (stage succeeds),
// rejected (stage fails), or ctx is cancelled (run deadline / cancel /
// stage timeout fires and the stage fails). Closes HS26-05-03 (approval
// gate) — formerly a fail-loud stub. With no StageApprovalService wired the
// gate cannot be persisted, so the stage fails loudly rather than
// silently auto-passing a human gate.
func (e *Executor) executeApproval(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) error {
	if e.stageApprovals == nil {
		return fmt.Errorf("approval stage %q: approval gate not configured", stage.Name)
	}

	gate, err := e.stageApprovals.Await(ctx, runID, stage.ID, stage.Config.RequiredApprovers)
	if err != nil {
		return fmt.Errorf("approval stage %q: open gate: %w", stage.Name, err)
	}

	// Persist a breadcrumb in the stage log so the run page (which tails
	// stage logs) explains why the stage is paused, and broadcast the
	// non-FSM "awaiting" status so the step rail tints + offers the
	// Approve/Reject buttons. The stage row itself stays "running" — the
	// stage FSM has no awaiting state, and the gate row is the source of
	// truth for the decision.
	if lw := e.newStageLineWriter(runID, stage); lw != nil {
		fmt.Fprintf(lw, "awaiting approval: %d distinct approver(s) required\n", gate.RequiredApprovers)
		lw.flush()
	}
	sr.Logs = fmt.Sprintf("awaiting approval: %d distinct approver(s) required\n", gate.RequiredApprovers)

	// If a previous attempt (pre-restart) already settled the gate, resolve
	// immediately without re-broadcasting awaiting.
	switch gate.Status {
	case model.StageApprovalApproved:
		return nil
	case model.StageApprovalRejected:
		return fmt.Errorf("approval stage %q: rejected by %s", stage.Name, gate.ResolvedBy)
	}

	e.broadcastStatus(runID, stage.ID, "awaiting")

	poll := e.approvalPoll
	if poll <= 0 {
		poll = defaultApprovalPoll
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Run deadline, operator cancel, or the per-stage timeout. Fail
			// the stage with the context cause; finalize() maps a canceled
			// run to Cancelled, a deadline to Failed.
			return fmt.Errorf("approval stage %q: %w", stage.Name, ctx.Err())
		case <-ticker.C:
			g, gerr := e.stageApprovals.Get(ctx, runID, stage.ID)
			if gerr != nil {
				// A transient store read error shouldn't fail the gate;
				// log and keep polling. A vanished gate (NotFound) is
				// unexpected mid-run but also non-fatal — keep waiting for
				// ctx to bound the loop.
				slog.Warn("approval stage: gate read failed", "run", runID, "stage", stage.Name, "err", gerr)
				continue
			}
			switch g.Status {
			case model.StageApprovalApproved:
				return nil
			case model.StageApprovalRejected:
				return fmt.Errorf("approval stage %q: rejected by %s", stage.Name, g.ResolvedBy)
			}
		}
	}
}

// executeCustom runs a Custom stage: a user-supplied shell script in an
// isolated container, pass/fail by exit code. The script is NEVER exec'd
// on the Cooker process — it runs in the container the stage runner
// provides (Kubernetes Job / docker run). Closes HS26-05-03 (custom script
// runner) — formerly a fail-loud stub. The UI collects the script
// (NodeConfigPanel `config.script`) and an image is required to run it in.
func (e *Executor) executeCustom(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) error {
	if stage.Config.Script == "" && len(stage.Config.Command) == 0 {
		return fmt.Errorf("custom stage %q: script or command is required", stage.Name)
	}
	if stage.Config.Image == "" {
		return fmt.Errorf("custom stage %q: image is required to run the script in a container", stage.Name)
	}
	return e.runStageContainer(ctx, runID, stage, sr, stagerunner.Request{
		Image:   stage.Config.Image,
		Command: stage.Config.Command,
		Script:  stage.Config.Script,
		Env:     stage.Config.Env,
	})
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
	ref := stage.Config.GitOpsRepo + "@" + stage.Config.GitOpsBranch
	sr.Artifacts = append(sr.Artifacts, model.Artifact{
		Type: "gitops-commit",
		Ref:  ref,
		// Digest holds the commit SHA — naming is approximate but
		// keeps one artifact shape across backends.
		Digest: res.CommitSHA,
	})
	// Inter-stage outputs (Primitive #3): expose commit/ref derived from
	// the existing result fields. GitOps has no adapter Outputs field.
	if outputsEnabled() {
		derived := map[string]string{}
		if res.CommitSHA != "" {
			derived["commit"] = res.CommitSHA
		}
		derived["ref"] = ref
		applyStageOutputs(sr, derived, nil)
	}
	return nil
}
