// Cooker — CI/CD management tool for OCI images and Kubernetes.
//
// @title           Cooker API
// @version         0.1
// @description     REST API for Cooker pipelines, environments, secrets, and apps.
// @contact.name    Cooker maintainers
// @contact.url     https://github.com/santapong/Cooker
// @license.name    Apache-2.0
// @license.url     https://www.apache.org/licenses/LICENSE-2.0
// @host            localhost:8080
// @BasePath        /api/v1
// @schemes         http https
// @securityDefinitions.apikey  BearerAuth
// @in              header
// @name            Authorization
// @description     OIDC-issued JWT bearer token. Obtain via the SPA's PKCE flow.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/server"
	"github.com/santapong/cooker/internal/store/postgres"
)

// Build-time metadata. Populated by the Makefile and GoReleaser via:
//
//	-ldflags "-X main.version=v0.1.0 -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// Defaults are "dev" so the binary still reports something useful
// when built without ldflags (e.g. `go run ./cmd/cooker`).
var (
	version = "v0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	// Subcommand dispatch. `cooker migrate up` applies embedded
	// migrations and exits without starting the server, so operators
	// (and `make migrate-up`) have an explicit migration step rather
	// than relying solely on the implicit apply-at-boot in NewStore.
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		os.Exit(runMigrate(os.Args[2:]))
	}

	printVersion := flag.Bool("version", false, "print build version and exit")
	flag.Parse()

	if *printVersion {
		fmt.Printf("version: %s\ncommit:  %s\ndate:    %s\n", version, commit, date)
		os.Exit(0)
	}

	// Propagate build metadata into the server package so the
	// /api/v1/version endpoint reflects real release information.
	server.BuildVersion = version
	server.BuildSHA = commit
	server.BuildTime = date

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	srv, err := server.New(cfg)
	if err != nil {
		slog.Error("failed to create server", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	addr := fmt.Sprintf(":%d", cfg.Port)
	slog.Info("cooker starting", "addr", addr, "env", cfg.Env)
	runErr := srv.RunContext(ctx, addr)
	if closeErr := srv.Close(); closeErr != nil {
		slog.Warn("shutdown: store close failed", "err", closeErr)
	}
	if runErr != nil {
		slog.Error("server error", "err", runErr)
		os.Exit(1)
	}
	slog.Info("cooker stopped cleanly")
}

// runMigrate applies embedded up-migrations against DATABASE_URL and
// returns a process exit code. Only "up" is supported: down-migrations
// are an explicit operator action, not a CLI convenience. Migrations are
// also applied implicitly at server boot; this command exists so the
// step can be run on its own (CI, one-off jobs, `make migrate-up`).
func runMigrate(args []string) int {
	direction := "up"
	if len(args) > 0 {
		direction = args[0]
	}
	if direction != "up" {
		slog.Error("migrate: only 'up' is supported", "got", direction)
		return 2
	}

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	slog.Info("migrate: applying migrations", "database", "DATABASE_URL")
	st, err := postgres.NewStore(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("migrate: failed", "err", err)
		return 1
	}
	if err := st.Close(); err != nil {
		slog.Warn("migrate: store close failed", "err", err)
	}
	slog.Info("migrate: done")
	return 0
}
