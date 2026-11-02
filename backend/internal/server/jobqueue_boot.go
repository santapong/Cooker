package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "github.com/lib/pq"

	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/notify/notifier"
	"github.com/santapong/cooker/internal/provider/vercel"
	"github.com/santapong/cooker/internal/scheduler"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// jobQueueDeps bundles the resources bootJobQueue spins up so the
// caller (Server.New) can store and tear them down in a single
// place. All fields are nil when COOKER_JOBQUEUE_ENABLED=false —
// the caller treats that as "no queue" and falls back to the inline
// RunPipeline path.
type jobQueueDeps struct {
	Pool     *jobqueue.Pool
	Store    jobqueue.Store
	Listener *jobqueue.PqListener
	DB       *sql.DB
	Enqueuer *service.JobQueueEnqueuer
	Runner   *service.JobQueueRunner
	AppQueue *service.AppDeployQueue
	Registry *jobqueue.Registry
}

// closeAll releases every resource held by the deps in reverse boot
// order. Safe to call when individual fields are nil.
func (d *jobQueueDeps) closeAll() {
	if d == nil {
		return
	}
	if d.Listener != nil {
		_ = d.Listener.Close()
	}
	if d.DB != nil {
		_ = d.DB.Close()
	}
}

// bootJobQueue is the integration point. Returns deps with every
// field nil when the feature flag is off so the caller's branching
// stays trivial (just check deps.Pool == nil). Opens its OWN *sql.DB
// rather than reaching into postgres.NewStore's internals — the
// listener already needs a dedicated connection, and a second small
// pool of 8 connections costs less than expanding the store API.
func bootJobQueue(ctx context.Context, cfg *config.Config, st *store.Store, exec *service.Executor, dispatcher *notifier.Dispatcher, apps ...*service.AppDeployer) (*jobQueueDeps, error) {
	if !cfg.JobQueue.Enabled {
		slog.Info("jobqueue: disabled", "flag", "COOKER_JOBQUEUE_ENABLED=false")
		return &jobQueueDeps{}, nil
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("jobqueue: enabled but DATABASE_URL is empty")
	}
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("jobqueue: open db: %w", err)
	}
	// Smaller pool than the main store. Worker count is the upper
	// bound on simultaneous Dequeue transactions; +2 idle covers the
	// stats / depth scraper without thrash.
	db.SetMaxOpenConns(cfg.JobQueue.Workers + 2)
	db.SetMaxIdleConns(2)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("jobqueue: ping: %w", err)
	}

	pgStore := jobqueue.NewPostgres(db)
	if dispatcher != nil && dispatcher.Outbox != nil {
		pgStore.BeforePrune = dispatcher.Outbox.CaptureTerminalJob
	}
	pgStore.OnTransition = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		if err := storepg.SyncJobRun(ctx, tx, j); err != nil {
			return err
		}
		return vercel.PublicationTransition(ctx, tx, j)
	}
	pgStore.OnEnqueue = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		if err := storepg.SyncRegistryAuthorityAdmission(ctx, tx, j); err != nil {
			return err
		}
		if err := scheduler.SyncOccurrenceAdmission(ctx, tx, j); err != nil {
			return err
		}
		if err := storepg.SyncTriggerAdmission(ctx, tx, j); err != nil {
			return err
		}
		return vercel.PublicationAdmission(ctx, tx, j)
	}

	// The notifier dispatcher is booted once, unconditionally, in
	// bootNotifier and shared here — the queue path and the inline
	// path emit through the same dispatcher.
	reg := jobqueue.NewRegistry()
	runner := service.NewJobQueueRunner(st, exec, dispatcher)
	reg.Register(service.JobKindPipelineRun, runner.Handle)
	var appQueue *service.AppDeployQueue
	if len(apps) > 0 && apps[0] != nil {
		d := apps[0]
		appQueue = &service.AppDeployQueue{Jobs: pgStore, Store: st, Deployer: d, ResolveCommit: d.Source.ResolveCommit, Deadline: runDeadline}
		reg.Register(service.JobKindAppDeploy, appQueue.Handle)
	}

	// PqListener is best-effort: a failure here means workers fall
	// back to PollEvery (5s default) instead of getting near-instant
	// NOTIFY wake-ups. Worth a Warn but not fatal — reliability is
	// preserved either way.
	listener, lErr := jobqueue.NewPqListener(cfg.DatabaseURL)
	if lErr != nil {
		slog.Warn("jobqueue: pq listener init failed; polling only", "err", lErr)
		listener = nil
	}

	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{
		Store:          pgStore,
		Registry:       reg,
		Size:           cfg.JobQueue.Workers,
		WorkerIDPrefix: cfg.JobQueue.WorkerIDPrefix,
		Notifier:       listenerOrNil(listener),
		OnHeartbeat:    service.HeartbeatJobRun(st),
		OnRecovery:     service.RecoverJobRun(st),
		OnComplete:     service.NotifyCompletedJob(st, dispatcher),
		ClaimContext: func(ctx context.Context, j *jobqueue.Job) context.Context {
			return store.WithRunClaim(ctx, j.ID, j.LockedBy, nil)
		},
	})
	if err != nil {
		if listener != nil {
			_ = listener.Close()
		}
		_ = db.Close()
		return nil, fmt.Errorf("jobqueue: new pool: %w", err)
	}

	enqueuer := service.NewJobQueueEnqueuer(pgStore, st.Pipelines)
	enqueuer.PrepareSecrets = exec.PreparePipelineSecrets

	slog.Info("jobqueue: enabled",
		"workers", cfg.JobQueue.Workers,
		"worker_prefix", cfg.JobQueue.WorkerIDPrefix,
		"listener", listener != nil)

	return &jobQueueDeps{
		Pool:     pool,
		Store:    pgStore,
		Listener: listener,
		DB:       db,
		Enqueuer: enqueuer,
		Runner:   runner,
		AppQueue: appQueue,
		Registry: reg,
	}, nil
}

// listenerOrNil unboxes a typed nil so jobqueue.PoolOptions.Notifier
// (interface field) sees a real nil rather than a typed-nil that
// passes the != nil check (a common Go interface gotcha).
func listenerOrNil(l *jobqueue.PqListener) jobqueue.Notifier {
	if l == nil {
		return nil
	}
	return l
}

// defaultNotifierRegistry builds the notifier.Registry with the
// channel adapters available in this build. Adding new adapters
// (Telegram, Microsoft Teams, etc.) is a one-line addition here.
func defaultNotifierRegistry() *notifier.Registry {
	r := notifier.NewRegistry()
	r.Register(notifier.NewSlackNotifier())
	r.Register(notifier.NewDiscordNotifier())
	r.Register(notifier.NewWebhookNotifier())
	r.Register(notifier.NewEmailNotifier())
	return r
}
