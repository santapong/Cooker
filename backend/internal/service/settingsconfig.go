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
	"github.com/santapong/cooker/internal/secrets"
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

// RegistryConfigService coordinates RegistryConfig writes, including
// the side-effect the handler must not do directly: writing the auth
// password to secrets.Manager and stamping only the reference onto the
// row. Nil-safe: when Secrets is nil, a Create that carries a password
// returns ErrConfigSecretsUnavailable; a credential-free Create
// succeeds through the plain store.
type RegistryConfigService struct {
	Store   *store.Store
	Secrets secrets.Manager
}

// NewRegistryConfigService constructs a RegistryConfigService.
func NewRegistryConfigService(s *store.Store, secs secrets.Manager) *RegistryConfigService {
	return &RegistryConfigService{Store: s, Secrets: secs}
}

// registryKeyRef returns the canonical PasswordRef for a registry id.
// Stable across renames; tied to the immutable config ID. Mirrors
// hostKeyRef.
func registryKeyRef(id string) string { return "registry:" + id }

// CreateRegistry persists a new RegistryConfig, optionally storing the
// supplied password into secrets.Manager and stamping the ref onto the
// record. The password bytes never live on the struct after this call.
func (s *RegistryConfigService) CreateRegistry(ctx context.Context, r *model.RegistryConfig, password []byte) error {
	if len(password) > 0 {
		if s.Secrets == nil {
			return ErrConfigSecretsUnavailable
		}
		if r.ID == "" {
			return fmt.Errorf("settingsconfig: registry ID required before storing password")
		}
		if err := s.ensureScopeEnv(ctx, registrySecretsEnvID, "_registries (internal)"); err != nil {
			return err
		}
		if err := s.Secrets.Put(ctx, registrySecretsEnvID, registryPasswordKey+"."+r.ID, password); err != nil {
			return fmt.Errorf("settingsconfig: store registry password: %w", err)
		}
		r.PasswordRef = registryKeyRef(r.ID)
	}
	return s.Store.Registries.Create(ctx, r)
}

// DeleteRegistry removes the registry and (best-effort) its stored
// password. A secrets-delete failure does not fail the row delete; an
// orphan secret entry can be reaped manually. Mirrors HostService.DeleteHost.
func (s *RegistryConfigService) DeleteRegistry(ctx context.Context, id string) error {
	if err := s.Store.Registries.Delete(ctx, id); err != nil {
		return err
	}
	if s.Secrets != nil {
		_ = s.Secrets.Delete(ctx, registrySecretsEnvID, registryPasswordKey+"."+id)
	}
	return nil
}

// ensureScopeEnv idempotently creates a synthetic Environment row used
// to scope per-config secrets in the database secrets backend. A no-op
// for backends that don't read the environment store (KeepSave, Vault,
// AWS, GCP). Mirrors HostService.ensureHostsEnv.
func (s *RegistryConfigService) ensureScopeEnv(ctx context.Context, envID, name string) error {
	return ensureScopeEnv(ctx, s.Store, envID, name)
}

// ClusterConfigService is the cluster-config analogue of
// RegistryConfigService. The secret it manages is the kubeconfig body.
type ClusterConfigService struct {
	Store   *store.Store
	Secrets secrets.Manager
}

// NewClusterConfigService constructs a ClusterConfigService.
func NewClusterConfigService(s *store.Store, secs secrets.Manager) *ClusterConfigService {
	return &ClusterConfigService{Store: s, Secrets: secs}
}

// clusterKeyRef returns the canonical KubeconfigRef for a cluster id.
func clusterKeyRef(id string) string { return "cluster:" + id }

// CreateCluster persists a new ClusterConfig, optionally storing the
// supplied kubeconfig into secrets.Manager and stamping the ref onto
// the record. The kubeconfig bytes never live on the struct after this
// call.
func (s *ClusterConfigService) CreateCluster(ctx context.Context, c *model.ClusterConfig, kubeconfig []byte) error {
	if len(kubeconfig) > 0 {
		if s.Secrets == nil {
			return ErrConfigSecretsUnavailable
		}
		if c.ID == "" {
			return fmt.Errorf("settingsconfig: cluster ID required before storing kubeconfig")
		}
		if err := ensureScopeEnv(ctx, s.Store, clusterSecretsEnvID, "_clusters (internal)"); err != nil {
			return err
		}
		if err := s.Secrets.Put(ctx, clusterSecretsEnvID, clusterKubeconfigKey+"."+c.ID, kubeconfig); err != nil {
			return fmt.Errorf("settingsconfig: store kubeconfig: %w", err)
		}
		c.KubeconfigRef = clusterKeyRef(c.ID)
	}
	return s.Store.Clusters.Create(ctx, c)
}

// DeleteCluster removes the cluster and (best-effort) its stored
// kubeconfig.
func (s *ClusterConfigService) DeleteCluster(ctx context.Context, id string) error {
	if err := s.Store.Clusters.Delete(ctx, id); err != nil {
		return err
	}
	if s.Secrets != nil {
		_ = s.Secrets.Delete(ctx, clusterSecretsEnvID, clusterKubeconfigKey+"."+id)
	}
	return nil
}

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
