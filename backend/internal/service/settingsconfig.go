// Package service — Settings registry/cluster-config CRUD business
// logic (audit finding HS26-05-04). Handler stays narrow (HTTP parsing
// only); this layer owns the side effect that the handler is forbidden
// from doing directly: writing the sensitive credential bytes (registry
// password, cluster kubeconfig) into secrets.Manager so the stored row
// carries only a reference. Mirrors HostService exactly.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// Synthetic envIDs scoping Settings-config secrets in secrets.Manager.
// Real environment IDs are UUIDs that never begin with an underscore,
// so these namespaces cannot collide. Mirrors hostSecretsEnvID.
const (
	registrySecretsEnvID = "_registries"
	clusterSecretsEnvID  = "_clusters"
)

// Secret key names within the per-config scope. One key per config,
// suffixed with the config ID (mirrors hostSSHKeyKey usage).
const (
	registryPasswordKey  = "registry_password"
	clusterKubeconfigKey = "cluster_kubeconfig"
)

// ErrConfigSecretsUnavailable is returned when a caller supplies
// credential bytes (registry password / cluster kubeconfig) but no
// secrets.Manager is configured. The handler maps this to HTTP 503 —
// same posture as ErrSecretsUnavailable for hosts.
var ErrConfigSecretsUnavailable = errors.New("settingsconfig: secrets manager not configured")

// ensureScopeEnv idempotently creates the synthetic Environment row
// that scopes per-config secrets in the database secrets backend. Free
// function shared by both config services; mirrors
// HostService.ensureHostsEnv. A no-op for backends that ignore the
// environment store.
func ensureScopeEnv(ctx context.Context, st *store.Store, envID, name string) error {
	if st == nil || st.Environments == nil {
		return nil
	}
	if _, err := st.Environments.Get(ctx, envID); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("settingsconfig: probe %s env: %w", envID, err)
	}
	env := &model.Environment{
		ID:        envID,
		Name:      name,
		CreatedAt: time.Now(),
	}
	if err := st.Environments.Create(ctx, env); err != nil {
		// Race with a concurrent create is fine; only surface genuine errors.
		if !errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("settingsconfig: bootstrap %s env: %w", envID, err)
		}
	}
	return nil
}
