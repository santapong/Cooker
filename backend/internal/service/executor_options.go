package service

import (
	"github.com/santapong/cooker/internal/builder"
	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/gitops"
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/pusher"
	"github.com/santapong/cooker/internal/stagerunner"
)

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

// WithDockerDeployer injects the deployer used by per-service deploy
// stages whose DeployRuntime is "docker" (Docker-host targets).
func WithDockerDeployer(d deployer.Deployer) Option {
	return func(e *Executor) {
		if d != nil {
			e.dockerDeployer = d
		}
	}
}

// WithComposeDeployer injects the deployer used by deploy stages whose
// DeployRuntime is "compose" (whole-stack docker compose up).
func WithComposeDeployer(d deployer.Deployer) Option {
	return func(e *Executor) {
		if d != nil {
			e.composeDeployer = d
		}
	}
}

// WithStatusBroadcaster installs a per-stage status broadcaster so the
// canvas tints nodes live as a run progresses. Pass nil to disable.
func WithStatusBroadcaster(b StatusBroadcaster) Option {
	return func(e *Executor) {
		e.statusBroadcast = b
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

// WithStageRunner injects the container runtime used by Test/Custom
// stages. Passing nil is a no-op (the default Noop runner is kept).
func WithStageRunner(r stagerunner.Runner) Option {
	return func(e *Executor) {
		if r != nil {
			e.stageRunner = r
		}
	}
}

// WithStageApprovals injects the approval-gate service used by approval
// stages. Nil leaves approval stages failing loudly (no silent auto-pass).
func WithStageApprovals(s *StageApprovalService) Option {
	return func(e *Executor) {
		e.stageApprovals = s
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

// WithLogStore installs the per-stage log history used for replay-on-
// connect (execution-observability redesign Part A). When set, each
// stage's complete log lines are appended (seq+ts stamped) so a WS
// subscriber that joins mid-run or reconnects with ?since=<seq> receives
// the backlog. Pass nil (or skip the option) to disable replay — live
// streaming via WithLogBroadcaster is unaffected.
func WithLogStore(s logstore.Store) Option {
	return func(e *Executor) {
		e.logStore = s
	}
}

// WithDeployGovernanceHook installs the pre-stage governance check for
// deploy stages (Milestone C executor hook). Nil disables it; pre-Milestone-C
// behaviour resumes.
func WithDeployGovernanceHook(h DeployGovernanceHook) Option {
	return func(e *Executor) {
		e.govHook = h
	}
}

