package server

import (
	"context"
	"database/sql"
	"log/slog"

	_ "github.com/lib/pq"

	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/templates"
)

// bootTemplates wires the Phase-2 pipeline-template catalog. Returns
// nil when DATABASE_URL is empty (memory-store dev mode) so the
// /templates endpoints respond 503 — same pattern as the secrets
// manager. The returned *sql.DB lifecycle is owned by the caller; we
// share the jobqueue's DB when it's been opened, otherwise we open a
// dedicated tiny pool.
func bootTemplates(ctx context.Context, cfg *config.Config, jobDeps *jobQueueDeps) (templates.Store, *sql.DB, error) {
	if cfg.DatabaseURL == "" {
		slog.Info("templates: disabled (no DATABASE_URL)")
		return nil, nil, nil
	}
	// Reuse the jobqueue's DB if it's already open — saves a connection
	// pool. The template lookups are cheap and infrequent.
	if jobDeps != nil && jobDeps.DB != nil {
		slog.Info("templates: enabled (shared jobqueue DB)")
		return templates.NewPostgresStore(jobDeps.DB), nil, nil
	}
	// Otherwise open a small dedicated pool.
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	slog.Info("templates: enabled (dedicated pool)")
	return templates.NewPostgresStore(db), db, nil
}
