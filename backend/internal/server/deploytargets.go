package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/deploy/deploytarget"
	"github.com/santapong/cooker/internal/deploy/deploytarget/cloudrun"
	"github.com/santapong/cooker/internal/deploy/deploytarget/ecs"
	"github.com/santapong/cooker/internal/deploy/deploytarget/flyio"
	"github.com/santapong/cooker/internal/deploy/deploytarget/render"
	sshtarget "github.com/santapong/cooker/internal/deploy/deploytarget/ssh"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
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

// registerSSHDeployTarget wires the SSH deploy-target adapter using
// the App and Host stores plus the host service for key resolution
// and TOFU pinning. Adds a cleanup that closes any cached SSH
// connections at process shutdown.
//
// Idempotent: if another caller already registered the SSH kind
// (e.g. unit-test path) we treat it as a no-op rather than fatal,
// mirroring registerDeployTargets's tryRegister behaviour.
func registerSSHDeployTarget(st *store.Store, hostSvc *service.HostService, cleanups *[]func()) {
	if st == nil || hostSvc == nil {
		slog.Info("ssh deploytarget: skipped (store or host service nil)")
		return
	}
	tgt := sshtarget.New()
	tgt.HostResolver = func(ctx context.Context, appID string) (*model.Host, error) {
		// One read per Deploy is fine — SSH connections are cached
		// by the adapter so the second-and-subsequent operations on
		// the same App reuse the same client.
		app, err := st.Apps.Get(ctx, appID)
		if err != nil {
			return nil, err
		}
		if app.DeployTarget.HostID == "" {
			return nil, fmt.Errorf("app %s has no DeployTarget.HostID", appID)
		}
		return st.Hosts.Get(ctx, app.DeployTarget.HostID)
	}
	tgt.PrivateKeyResolver = hostSvc.LoadPrivateKey
	tgt.PinHostKey = hostSvc.PinHostKey
	// When a Host is deleted, evict any cached SSH client so the
	// adapter doesn't pin a TCP socket to a row that no longer
	// exists. The service exposes this as a callback so it doesn't
	// have to import the ssh adapter package.
	hostSvc.OnDelete = tgt.Evict

	if err := deploytarget.Register(tgt); err != nil {
		if errors.Is(err, deploytarget.ErrDuplicateKind) {
			return
		}
		slog.Warn("ssh deploytarget: register failed", "err", err)
		return
	}
	slog.Info("deploytarget registered", "name", "ssh")
	if cleanups != nil {
		*cleanups = append(*cleanups, tgt.CloseAll)
	}
}

// sshHostLister adapts *store.Store to config.SSHHostLister. Lives
// here so config doesn't have to import store (which would create
// an import cycle).
type sshHostLister struct {
	st *store.Store
}

func (l sshHostLister) ListSSHHostsLaxStrictHostKey(ctx context.Context) ([]config.SSHHostSummary, error) {
	if l.st == nil || l.st.Hosts == nil {
		return nil, nil
	}
	all, err := l.st.Hosts.List(ctx, 0, 0)
	if err != nil {
		return nil, err
	}
	var lax []config.SSHHostSummary
	for _, h := range all {
		if h.Kind != model.HostKindSSHDocker {
			continue
		}
		if !h.SSHStrictHostKey {
			lax = append(lax, config.SSHHostSummary{ID: h.ID, Name: h.Name})
		}
	}
	return lax, nil
}
