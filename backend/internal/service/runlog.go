package service

import (
	"context"
	"log/slog"
)

// runLoggerKey is the unexported context-value key used to thread a
// run-bound *slog.Logger through Execute and into the per-stage
// callbacks. Callers that already have a logger should keep using
// it directly; this exists so deeper helpers (gitops, app-deployer
// callouts) can pick it up without taking it as an argument.
type runLoggerKey struct{}

// withRunLogger attaches logger to ctx. Used by service.Executor
// so every goroutine spawned by the DAG runner emits log lines
// already labelled with the run id.
func withRunLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, runLoggerKey{}, logger)
}

// LoggerFrom returns the run-bound logger if one is attached, or
// slog.Default() otherwise. Safe to call from any package that
// has been handed an executor-derived ctx.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(runLoggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
