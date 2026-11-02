package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/santapong/cooker/internal/logstore"
)

// bootLogStore requires migrations to have run through the main Postgres
// store. An explicit durable selection must fail boot rather than silently
// degrading to volatile memory. Empty selects Postgres with DATABASE_URL.
func bootLogStore(ctx context.Context, databaseURL string) (logstore.Store, func(), error) {
	backend := os.Getenv("COOKER_LOGSTORE_BACKEND")
	if backend == "" {
		if databaseURL != "" {
			backend = "postgres"
		} else {
			backend = "memory"
		}
	}
	if backend == "memory" {
		return logstore.FromEnv(), func() {}, nil
	}
	if backend != "postgres" {
		return nil, nil, fmt.Errorf("unsupported COOKER_LOGSTORE_BACKEND %q", backend)
	}
	if databaseURL == "" {
		return nil, nil, fmt.Errorf("postgres log history requires DATABASE_URL")
	}
	limits := logstore.Limits{}
	for key, dst := range map[string]*int{"COOKER_LOGSTORE_MAX_BYTES": &limits.MaxBytesPerStage, "COOKER_LOGSTORE_MAX_STREAMS": &limits.MaxStreams} {
		if raw := os.Getenv(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				return nil, nil, fmt.Errorf("%s must be a positive integer", key)
			}
			*dst = n
		}
	}
	if raw := os.Getenv("COOKER_LOGSTORE_RETENTION"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return nil, nil, fmt.Errorf("COOKER_LOGSTORE_RETENTION must be a positive duration")
		}
		limits.Retention = d
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)
	p := logstore.NewPostgres(db, limits)
	bootCtx, cancelBoot := context.WithTimeout(ctx, 5*time.Second)
	err = p.Prune(bootCtx)
	cancelBoot()
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("durable stage logs unavailable (check database migrations)")
	}
	pruneCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-pruneCtx.Done():
				return
			case <-ticker.C:
				c, stop := context.WithTimeout(pruneCtx, 5*time.Second)
				err := p.Prune(c)
				stop()
				if err != nil && pruneCtx.Err() == nil {
					slog.Warn("stage log retention cleanup failed")
				}
			}
		}
	}()
	return p, func() { cancel(); <-done; _ = db.Close() }, nil
}
