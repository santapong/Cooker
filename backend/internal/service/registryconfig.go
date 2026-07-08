package service

import (
	"context"
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
)

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
