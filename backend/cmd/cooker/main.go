// Cooker — CI/CD management tool for OCI images and Kubernetes.
//
// @title           Cooker API
// @version         0.1
// @description     REST API for Cooker pipelines, environments, secrets, and apps.
// @contact.name    Cooker maintainers
// @contact.url     https://github.com/cooker-ci/cooker
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
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cooker-ci/cooker/internal/config"
	"github.com/cooker-ci/cooker/internal/server"
)

func main() {
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
