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

//go:embed migrations/*.sql
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
		t := time.NewTimer(jittered)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
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

// applyMigrations runs each embedded *.up.sql file that hasn't yet
// been applied, recording each in the schema_migrations table so a
// re-run is idempotent at the file granularity (rather than relying
// on every CREATE TABLE IF NOT EXISTS to be perfectly idempotent).
//
// Down migrations are embedded too (*.down.sql) so a future
// rollback CLI / test harness can call Rollback. They are not
// invoked at boot — production rollbacks are an explicit operator
// action.
func applyMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("postgres: create schema_migrations: %w", err)
	}
	applied, err := loadAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	upFiles, err := listMigrationFiles(".up.sql")
	if err != nil {
		return err
	}
	for _, name := range upFiles {
		version := strings.TrimSuffix(name, ".up.sql")
		if applied[version] {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("postgres: read %s: %w", name, err)
		}
		// Wrap each migration in a transaction so a partial failure
		// can't half-apply the file and leave schema_migrations
		// out of sync with the actual state.
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("postgres: begin %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("postgres: apply %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("postgres: record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("postgres: commit %s: %w", name, err)
		}
	}
	return nil
}

// rollbackMigration applies the .down.sql for the given version and
// removes the version from schema_migrations. Exposed for tests
// (and a future CLI flag); not invoked at boot. Returns
// errMigrationNotApplied if the version isn't currently recorded.
var errMigrationNotApplied = fmt.Errorf("migration not applied")

func rollbackMigration(ctx context.Context, db *sql.DB, version string) error {
	applied, err := loadAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	if !applied[version] {
		return fmt.Errorf("postgres: %s: %w", version, errMigrationNotApplied)
	}
	sqlBytes, err := migrationsFS.ReadFile("migrations/" + version + ".down.sql")
	if err != nil {
		return fmt.Errorf("postgres: read %s.down.sql: %w", version, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin rollback %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("postgres: rollback %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=$1`, version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("postgres: unrecord %s: %w", version, err)
	}
	return tx.Commit()
}

// loadAppliedMigrations returns the set of version strings already
// recorded in schema_migrations.
func loadAppliedMigrations(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("postgres: read schema_migrations: %w", err)
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

// listMigrationFiles returns the embedded migration filenames with
// the given suffix (".up.sql" or ".down.sql"), sorted.
func listMigrationFiles(suffix string) ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("postgres: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
