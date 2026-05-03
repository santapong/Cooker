// Package postgres provides PostgreSQL-backed implementations of the
// store interfaces. Connect via NewStore; migrations embedded from
// the migrations/ subdirectory run automatically on open.
package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/cooker-ci/cooker/internal/observability"
	"github.com/cooker-ci/cooker/internal/store"
)

// pingBudget caps how long NewStore waits for the database to become
// reachable before giving up. K8s readiness probes ride out the
// per-attempt failures via the readiness endpoint.
var (
	pingBudget       = 5 * time.Minute
	pingInitialDelay = 500 * time.Millisecond
	pingMaxDelay     = 30 * time.Second
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

// NewStore opens a PostgreSQL connection, pings it, applies embedded
// up-migrations, and returns an aggregate store. Fails fast on any
// connectivity or migration error so Cooker refuses to boot into an
// inconsistent state.
func NewStore(ctx context.Context, databaseURL string) (*store.Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}
	db.SetConnMaxLifetime(time.Hour)
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := pingWithBackoff(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	if err := applyMigrations(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return store.New(
		NewPipelineStore(db),
		NewRunStore(db),
		NewEnvironmentStore(db),
		NewAppStore(db),
		NewHostStore(db),
		NewUserStore(db),
		db.Close,
		db.PingContext,
	), nil
}

// pingWithBackoff retries Ping with jittered exponential backoff up to
// pingBudget. Each attempt has a 2s timeout; the loop exits cleanly if
// the parent context is cancelled (e.g. SIGTERM during boot).
func pingWithBackoff(ctx context.Context, db *sql.DB) error {
	deadline := time.Now().Add(pingBudget)
	delay := pingInitialDelay
	attempt := 0
	for {
		attempt++
		attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := db.PingContext(attemptCtx)
		cancel()
		if err == nil {
			if attempt > 1 {
				slog.Info("postgres: connected", "attempts", attempt)
			}
			return nil
		}
		observability.IncDBConnectionError()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().Add(delay).After(deadline) {
			return fmt.Errorf("budget exhausted after %d attempts: %w", attempt, err)
		}
		jittered := delay + time.Duration(rand.Int63n(int64(delay/2+1)))
		slog.Warn("postgres: ping failed, retrying", "attempt", attempt, "delay", jittered.String(), "err", err)
		select {
		case <-time.After(jittered):
		case <-ctx.Done():
			return ctx.Err()
		}
		if delay < pingMaxDelay {
			delay *= 2
			if delay > pingMaxDelay {
				delay = pingMaxDelay
			}
		}
	}
}

// applyMigrations runs each embedded *.up.sql file in filename order.
// Statements use IF NOT EXISTS and are safe to re-apply.
func applyMigrations(ctx context.Context, db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("postgres: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("postgres: read %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("postgres: apply %s: %w", name, err)
		}
	}
	return nil
}
