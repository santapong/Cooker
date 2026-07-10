package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

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
	DB *sql.DB
}

func (d *notifierDeps) closeAll() {
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
	// Dispatch is bursty and short-lived: ListEnabled once per event.
	// Two connections cover concurrent events without another big pool.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("notifier: ping: %w", err)
	}
	if !codec.Active() {
		slog.Warn("notifier: COOKER_SECRET_KEY not set; notification-target configs will be stored unencrypted")
	}
	ts := notifier.NewPostgresTargetStore(db).WithCodec(codec)
	return &notifierDeps{
		Dispatcher:  notifier.NewDispatcher(ts, defaultNotifierRegistry()),
		TargetStore: ts,
		DB:          db,
	}, nil
}
