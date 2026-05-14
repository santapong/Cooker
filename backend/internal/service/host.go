// Package service — host-CRUD business logic. Handler stays narrow
// (HTTP parsing only); this layer owns the side effects: PEM bytes
// are written to secrets.Manager and the Host carries only a
// reference. Mirrors the WebhookSecret pattern in app.go where the
// codec writes ciphertext into the store column.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
)

// hostSecretsEnvID is the synthetic envID used to scope host-level
// secrets in the secrets.Manager. Real environment IDs are UUIDs
// which never begin with an underscore, so this namespace cannot
// collide.
const hostSecretsEnvID = "_hosts"

// hostSSHKeyKey is the key under which a host's SSH private key
// lives in secrets.Manager. One key per host.
const hostSSHKeyKey = "ssh_private_key"

// HostService coordinates Host writes — including the side-effects
// the handler is forbidden from doing directly (writing PEM bytes
// to secrets.Manager, validating the key parses before storage).
//
// Stores nil-safe: when Secrets is nil, SSH host operations return
// errSecretsUnavailable. The handler maps that to HTTP 503.
type HostService struct {
	Store   *store.Store
	Secrets secrets.Manager
	// OnDelete, if non-nil, is invoked after a successful DeleteHost
	// with the host ID. The server wires this to the SSH adapter's
	// Evict so deleting a host drops its cached SSH connection. nil
	// is safe (skipped).
	OnDelete func(hostID string)
}

// NewHostService constructs a HostService.
func NewHostService(s *store.Store, secs secrets.Manager) *HostService {
	return &HostService{Store: s, Secrets: secs}
}

// ErrSecretsUnavailable is returned when a caller tries to store
// an SSH private key but no secrets.Manager is configured.
var ErrSecretsUnavailable = errors.New("hostservice: secrets manager not configured")

// ErrInvalidPrivateKey is returned when the supplied PEM body is
// malformed or does not contain an ssh.Signer-acceptable key.
var ErrInvalidPrivateKey = errors.New("hostservice: invalid private key")

// hostKeyRef returns the canonical SSHPrivateKeyRef for h. Stable
// across renames; tied to the immutable Host ID.
func hostKeyRef(hostID string) string {
	return "host:" + hostID
}

// resolveKeyRef parses a ref produced by hostKeyRef back to its
// host ID. Returns "" if the ref doesn't match the expected form.
func resolveKeyRef(ref string) string {
	const prefix = "host:"
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}
	return strings.TrimPrefix(ref, prefix)
}

// CreateHost persists a new Host, optionally storing the supplied
// PEM private key into secrets.Manager and stamping the ref onto
// the host record. The PEM bytes never live on the Host struct
// after this call returns.
func (s *HostService) CreateHost(ctx context.Context, h *model.Host, pemPrivateKey []byte) error {
	if err := s.maybeStoreKey(ctx, h, pemPrivateKey); err != nil {
		return err
	}
	return s.Store.Hosts.Create(ctx, h)
}

// UpdateHost persists changes to an existing Host. If pemPrivateKey
// is non-empty, the key is re-written through secrets.Manager.
// Otherwise the existing ref carries forward unchanged.
//
// existing must be the row currently in the store (Get-then-Update
// flow). UpdateHost preserves existing.SSHPrivateKeyRef and
// existing.SSHKnownHostKey unless h overrides them explicitly.
func (s *HostService) UpdateHost(ctx context.Context, existing, h *model.Host, pemPrivateKey []byte) error {
	// Preserve the secret-ref by default — the handler does not see
	// it (json:"-") and so cannot pass it back through the request body.
	if h.SSHPrivateKeyRef == "" {
		h.SSHPrivateKeyRef = existing.SSHPrivateKeyRef
	}
	// SSHKnownHostKey is pinned by the SSH adapter on first connect;
	// preserve it across an Update unless the operator deliberately
	// cleared it (e.g. to re-do TOFU after a legitimate host rotation).
	if h.SSHKnownHostKey == "" {
		h.SSHKnownHostKey = existing.SSHKnownHostKey
	}
	if len(pemPrivateKey) > 0 {
		if err := s.maybeStoreKey(ctx, h, pemPrivateKey); err != nil {
			return err
		}
	}
	return s.Store.Hosts.Update(ctx, h)
}

// DeleteHost removes the host and (best-effort) its private key.
// A secrets-delete failure does not fail the host delete; the
// orphan key entry can be reaped manually if it ever happens.
// After a successful delete OnDelete is invoked so the SSH adapter
// can evict any cached connection to this host.
func (s *HostService) DeleteHost(ctx context.Context, hostID string) error {
	if err := s.Store.Hosts.Delete(ctx, hostID); err != nil {
		return err
	}
	if s.Secrets != nil {
		_ = s.Secrets.Delete(ctx, hostSecretsEnvID, hostSSHKeyKey+"."+hostID)
	}
	if s.OnDelete != nil {
		s.OnDelete(hostID)
	}
	return nil
}

// LoadPrivateKey resolves the PEM body for the SSH host. The
// adapter calls this through Target.PrivateKeyResolver. Returns
// ErrSecretsUnavailable if the manager is not configured.
func (s *HostService) LoadPrivateKey(ctx context.Context, host *model.Host) ([]byte, error) {
	if host == nil || host.SSHPrivateKeyRef == "" {
		return nil, fmt.Errorf("hostservice: host %s has no SSHPrivateKeyRef", safeHostID(host))
	}
	hostID := resolveKeyRef(host.SSHPrivateKeyRef)
	if hostID == "" {
		return nil, fmt.Errorf("hostservice: unrecognised key ref %q", host.SSHPrivateKeyRef)
	}
	if s.Secrets == nil {
		return nil, ErrSecretsUnavailable
	}
	pem, err := s.Secrets.Get(ctx, hostSecretsEnvID, hostSSHKeyKey+"."+hostID)
	if err != nil {
		return nil, fmt.Errorf("hostservice: load ssh key for host %s: %w", hostID, err)
	}
	return pem, nil
}

// PinHostKey persists a TOFU-captured host key onto the Host row.
// Called by the SSH adapter the first time it successfully connects
// to a !strict host. Errors here are non-fatal to a deploy (the
// connection already succeeded); the caller logs them.
//
// The optimistic-concurrency Update can lose a race against an
// operator editing the host in parallel — that returns ErrConflict
// because the version no longer matches the snapshot we just Got.
// We retry up to pinHostKeyMaxRetries times with a fresh Get each
// time, so a sustained edit window can't permanently prevent the
// pin from being persisted.
func (s *HostService) PinHostKey(ctx context.Context, host *model.Host, serialisedKey string) error {
	if host == nil {
		return fmt.Errorf("hostservice: nil host")
	}
	for attempt := 0; attempt < pinHostKeyMaxRetries; attempt++ {
		existing, err := s.Store.Hosts.Get(ctx, host.ID)
		if err != nil {
			return err
		}
		if existing.SSHKnownHostKey == serialisedKey {
			// Already pinned to the same value (concurrent first-
			// connect or earlier retry). Done.
			return nil
		}
		if existing.SSHKnownHostKey != "" {
			// Someone else pinned a *different* key already; refuse
			// to silently overwrite — this is the scenario the TOFU
			// policy exists to flag.
			return fmt.Errorf("hostservice: refusing to overwrite pinned host key for %s", host.ID)
		}
		existing.SSHKnownHostKey = serialisedKey
		err = s.Store.Hosts.Update(ctx, existing)
		if err == nil {
			return nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return err
		}
		// ErrConflict: operator edited the row between our Get and
		// Update. Re-Get and try again.
	}
	return fmt.Errorf("hostservice: failed to pin host key for %s after %d retries (concurrent edits)",
		host.ID, pinHostKeyMaxRetries)
}

// pinHostKeyMaxRetries bounds the PinHostKey retry loop. Three is
// enough to absorb a typical operator burst-edit without making the
// adapter sit on a stuck pin call forever.
const pinHostKeyMaxRetries = 3

// maybeStoreKey writes pemPrivateKey to secrets.Manager under the
// Host's ref, validating the key parses before storage. Sets
// h.SSHPrivateKeyRef on success. No-op when pem is empty.
func (s *HostService) maybeStoreKey(ctx context.Context, h *model.Host, pem []byte) error {
	if len(pem) == 0 {
		return nil
	}
	if s.Secrets == nil {
		return ErrSecretsUnavailable
	}
	if _, err := gossh.ParsePrivateKey(pem); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
	}
	if h.ID == "" {
		return fmt.Errorf("hostservice: host ID required before storing key")
	}
	// The database secrets backend stores ciphertext on the
	// Environment row keyed by envID. Our synthetic "_hosts" envID
	// must therefore exist as a real row before Put will succeed.
	// Idempotently create it. Backends that don't use the store
	// path (KeepSave, Vault, AWS, GCP) ignore the env row entirely;
	// the Create is harmless.
	if err := s.ensureHostsEnv(ctx); err != nil {
		return err
	}
	if err := s.Secrets.Put(ctx, hostSecretsEnvID, hostSSHKeyKey+"."+h.ID, pem); err != nil {
		return fmt.Errorf("hostservice: store ssh key: %w", err)
	}
	h.SSHPrivateKeyRef = hostKeyRef(h.ID)
	return nil
}

// ensureHostsEnv idempotently creates the synthetic "_hosts"
// Environment row used to scope per-host secrets in the database
// secrets backend. A no-op for backends that don't read the
// environment store (KeepSave, Vault, AWS, GCP).
func (s *HostService) ensureHostsEnv(ctx context.Context) error {
	if s.Store == nil || s.Store.Environments == nil {
		return nil
	}
	if _, err := s.Store.Environments.Get(ctx, hostSecretsEnvID); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("hostservice: probe hosts env: %w", err)
	}
	env := &model.Environment{
		ID:        hostSecretsEnvID,
		Name:      "_hosts (internal)",
		CreatedAt: time.Now(),
	}
	if err := s.Store.Environments.Create(ctx, env); err != nil {
		// Race with a concurrent create is fine; only surface
		// genuine errors.
		if !errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("hostservice: bootstrap hosts env: %w", err)
		}
	}
	return nil
}

func safeHostID(h *model.Host) string {
	if h == nil {
		return "<nil>"
	}
	return h.ID
}
