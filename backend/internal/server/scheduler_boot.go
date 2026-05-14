package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/scheduler"
)

// schedulerDeps bundles the Phase-2 cron scheduler so the caller
// (Server.New) can store and tear it down in a single place. All
// fields are nil when the scheduler is disabled.
type schedulerDeps struct {
	Runner *scheduler.Runner
	Store  scheduler.Store
}

// bootScheduler wires the cron scheduler. Requires the jobqueue to
// be enabled because the scheduler enqueues pipeline-run jobs via
// the JobQueueEnqueuer. When either feature is off, returns an
// all-nil deps and the caller skips Runner.Run.
func bootScheduler(_ context.Context, cfg *config.Config, jobDeps *jobQueueDeps) (*schedulerDeps, error) {
	if !cfg.Scheduler.Enabled {
		slog.Info("scheduler: disabled", "flag", "COOKER_SCHEDULER_ENABLED=false")
		return &schedulerDeps{}, nil
	}
	if jobDeps == nil || jobDeps.DB == nil || jobDeps.Enqueuer == nil {
		return nil, fmt.Errorf("scheduler: requires jobqueue (set COOKER_JOBQUEUE_ENABLED=true)")
	}
	store := scheduler.NewPostgresStore(jobDeps.DB)
	runner, err := scheduler.NewRunner(scheduler.Options{
		DB:        jobDeps.DB,
		Store:     store,
		Enqueuer:  jobDeps.Enqueuer,
		TickEvery: cfg.Scheduler.TickEvery,
	})
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}
	slog.Info("scheduler: enabled", "tick_every", cfg.Scheduler.TickEvery.String())
	return &schedulerDeps{Runner: runner, Store: store}, nil
}
