// Package postgres provides PostgreSQL-backed implementations of the
// store interfaces. Connect via NewStore; migrations embedded from
// the migrations/ subdirectory run automatically on open.
package postgres

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"embed"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/santapong/cooker/internal/observability"
	"github.com/santapong/cooker/internal/store"
)

// osEnvGetter is an indirection so envLookup can be stubbed in tests.
var osEnvGetter = os.Getenv

// jitter returns a non-negative duration in [0, d/2]. crypto/rand
// avoids the locked global math/rand source.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	max := int64(d/2) + 1
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return 0
	}
	n := int64(binary.BigEndian.Uint64(b[:]) & ((1 << 62) - 1))
	return time.Duration(n % max)
}

// pingBudget caps how long NewStore waits for the database to become
// reachable before giving up. K8s readiness probes ride out the
// per-attempt failures via the readiness endpoint.
var (
	pingBudget       = 5 * time.Minute
	pingInitialDelay = 500 * time.Millisecond
	pingMaxDelay     = 30 * time.Second
)

// Connection pool tunables. Defaults match the historical
// hard-coded values; operators can override via env vars without
// editing the chart. Sizing guidance:
//   - MaxOpenConns × replicaCount must stay below Postgres's own
//     max_connections (default 100). 25 × 3 = 75 leaves headroom.
//   - MaxIdleConns ≤ MaxOpenConns. Tune up if you see frequent
//     "connection idle for >X" reconnects.
//   - ConnMaxLifetime should be < the load-balancer's idle timeout
//     so dead connections don't accumulate.
const (
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = time.Hour
)

func envInt(name string, fallback int) int {
	if v := strings.TrimSpace(envLookup(name)); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func envDuration(name string, fallback time.Duration) time.Duration {
	if v := strings.TrimSpace(envLookup(name)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

// envLookup is a small indirection so tests can stub the lookup.
var envLookup = func(name string) string { return osEnvGetter(name) }

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
	db.SetConnMaxLifetime(envDuration("COOKER_DB_CONN_MAX_LIFETIME", defaultConnMaxLifetime))
	db.SetMaxOpenConns(envInt("COOKER_DB_MAX_OPEN_CONNS", defaultMaxOpenConns))
	db.SetMaxIdleConns(envInt("COOKER_DB_MAX_IDLE_CONNS", defaultMaxIdleConns))

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
		NewPromotionStore(db),
		NewAppStore(db),
		NewAppDeployStore(db),
		NewAuditEventStore(db),
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
		jittered := delay + jitter(delay)
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

// migrationLockKey is the bigint key passed to pg_advisory_lock
// while migrations run. Picked deterministically (any 64-bit
// constant works) so two replicas booting simultaneously contend
// on the same lock and serialise their migration loops. The lock
// is session-scoped and released on connection close, which is
// what we want: a crash mid-migration releases the lock cleanly.
//
// Value derived from "cooker-migration" via a fixed hash so it's
// stable across builds — but operators on shared databases who
// want to override it can recompute and patch this constant.
const migrationLockKey int64 = 0x434f4f4b4552_4d49 // "COOKER_MI"

// applyMigrations runs each embedded *.up.sql file that hasn't yet
// been applied, recording each in the schema_migrations table so a
// re-run is idempotent at the file granularity (rather than relying
// on every CREATE TABLE IF NOT EXISTS to be perfectly idempotent).
//
// pg_advisory_lock serialises concurrent boots: only one replica
// runs the migration body at a time, so non-idempotent DDL added
// in a future migration won't fire twice in the same window.
//
// Down migrations are embedded too (*.down.sql) so a future
// rollback CLI / test harness can call Rollback. They are not
// invoked at boot — production rollbacks are an explicit operator
// action.
func applyMigrations(ctx context.Context, db *sql.DB) error {
	// Hold the migration lock on a single connection for the
	// duration of the loop. database/sql may otherwise hand each
	// Exec a different connection from the pool, defeating the
	// session-scoped advisory lock.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("postgres: acquire migration conn: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("postgres: pg_advisory_lock: %w", err)
	}
	defer func() {
		// Best-effort unlock; the connection close will release
		// it anyway, but explicit release lets a long-lived pool
		// reuse the connection without holding the lock.
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockKey)
	}()
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("postgres: create schema_migrations: %w", err)
	}
	applied, err := loadAppliedMigrationsConn(ctx, conn)
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
		// out of sync with the actual state. The transaction runs
		// on the same conn that holds the advisory lock.
		tx, err := conn.BeginTx(ctx, nil)
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
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return loadAppliedMigrationsConn(ctx, conn)
}

func loadAppliedMigrationsConn(ctx context.Context, conn *sql.Conn) (map[string]bool, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version FROM schema_migrations`)
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
