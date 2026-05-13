package server

import (
	"errors"
	"log/slog"

	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/deploytarget"
	"github.com/santapong/cooker/internal/deploytarget/cloudrun"
	"github.com/santapong/cooker/internal/deploytarget/ecs"
	"github.com/santapong/cooker/internal/deploytarget/flyio"
	"github.com/santapong/cooker/internal/deploytarget/render"
)

// registerDeployTargets registers the cloud deploy-target adapters
// that have non-empty configuration. Targets without credentials are
// skipped so operators only opt in to backends they actually use.
func registerDeployTargets(cfg config.DeployTargetsConfig) {
	tryRegister := func(name string, t deploytarget.Target) {
		if err := deploytarget.Register(t); err != nil {
			if errors.Is(err, deploytarget.ErrDuplicateKind) {
				return // already registered earlier in this process
			}
			slog.Warn("deploytarget register failed", "name", name, "err", err)
			return
		}
		slog.Info("deploytarget registered", "name", name)
	}
	if cfg.CloudRunProject != "" && cfg.CloudRunRegion != "" {
		tryRegister("cloud-run", cloudrun.New(cfg.CloudRunProject, cfg.CloudRunRegion))
	}
	if cfg.ECSRegion != "" && cfg.ECSCluster != "" {
		t := ecs.New(cfg.ECSRegion, cfg.ECSCluster)
		t.ExecutionRole = cfg.ECSExecutionRole
		t.TaskRole = cfg.ECSTaskRole
		t.Subnets = cfg.ECSSubnets
		t.SecurityGroups = cfg.ECSSecurityGroups
		tryRegister("ecs", t)
	}
	if cfg.FlyToken != "" {
		tryRegister("fly", flyio.New(cfg.FlyToken, cfg.FlyRegion))
	}
	if cfg.RenderToken != "" {
		tryRegister("render", render.New(cfg.RenderToken, cfg.RenderOwnerID))
	}
}
