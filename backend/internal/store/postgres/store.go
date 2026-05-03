// Package postgres provides PostgreSQL-backed implementations of the
// store interfaces. Connect via NewStore; migrations embedded from
// the migrations/ subdirectory run automatically on open.
package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/cooker-ci/cooker/internal/store"
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

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
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
	), nil
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
