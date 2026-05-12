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

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

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
