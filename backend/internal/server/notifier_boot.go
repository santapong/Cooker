package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/notify/notifier"
)

// notifierDeps bundles the always-on notification plumbing. Unlike
// the job queue this boots unconditionally: notifications must work
// on a default install (the inline run path, app deploys, canary
// transitions), not only when COOKER_JOBQUEUE_ENABLED=true. An empty
// notification_targets table makes every dispatch a no-op, so there
// is no cost until an operator configures a target.
type notifierDeps struct {
	Dispatcher  *notifier.Dispatcher
	TargetStore notifier.TargetStore
	// DB is the dedicated small pool behind the Postgres target
	// store; nil when the memory fallback is in use.
	DB     *sql.DB
	Outbox *notifier.Outbox
	cancel context.CancelFunc
	done   chan struct{}
}

func (d *notifierDeps) start() {
	if d == nil || d.Outbox == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.done = make(chan struct{})
	go func() { defer close(d.done); d.Outbox.Run(ctx) }()
}

func (d *notifierDeps) closeAll() {
	if d != nil && d.cancel != nil {
		d.cancel()
		select {
		case <-d.done:
		case <-time.After(6 * time.Second):
			slog.Warn("notifier: delivery drain timed out; pending claims will require inspection")
		}
	}
	if d != nil && d.DB != nil {
		_ = d.DB.Close()
	}
}

// bootNotifier wires the target store + dispatcher. With a
// DATABASE_URL the targets live in Postgres (migration 011) and the
// config column is sealed with the same AES-GCM codec as Cooker's
// other secrets; without one (memory-store dev mode) targets are
// process-local so the admin API and UI still work, just without
// persistence across restarts.
func bootNotifier(ctx context.Context, cfg *config.Config, codec *crypto.Codec) (*notifierDeps, error) {
	if cfg.DatabaseURL == "" {
		slog.Info("notifier: no DATABASE_URL; using in-memory target store")
		ts := notifier.NewMemoryTargetStore()
		return &notifierDeps{
			Dispatcher:  notifier.NewDispatcher(ts, defaultNotifierRegistry()),
			TargetStore: ts,
		}, nil
	}
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("notifier: open db: %w", err)
	}
	// Acceptance, reconciliation, target resolution, and status reads share this
	// small pool. External sends hold no SQL transaction or target-row lock.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("notifier: ping: %w", err)
	}
	if !codec.Active() {
		slog.Warn("notifier: COOKER_SECRET_KEY not set; notification-target configs will be stored unencrypted")
	}
	ts := notifier.NewPostgresTargetStore(db).WithCodec(codec)
	registry := defaultNotifierRegistry()
	outbox := notifier.NewOutbox(db, ts, registry)
	dispatcher := notifier.NewDispatcher(ts, registry)
	dispatcher.Outbox = outbox
	return &notifierDeps{
		Dispatcher:  dispatcher,
		TargetStore: ts,
		Outbox:      outbox,
		DB:          db,
	}, nil
}
