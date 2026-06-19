// Package vault implements secrets.Manager backed by HashiCorp Vault's
// KV v2 secrets engine. The Cooker environment ID + key map to a path
// segment under a configurable mount + prefix, so each Cooker secret
// becomes its OWN Vault secret at <mount>/<prefix>/<envID>/<key>, with
// the value stored under a single "value" field.
//
// This per-key layout (vs the older one-secret-per-environment map)
// eliminates the read-modify-write race that the previous design had:
// concurrent Put/Delete of different keys in the same environment used
// to read the whole map, mutate one field, and write it back, so two
// in-flight writers could clobber each other and silently drop a
// secret (audit CR-2). With one Vault path per key, each Put/Delete is
// a single atomic write/delete against its own path — no read, no
// merge, no lost update.
//
// NOTE (layout change): this is NOT wire-compatible with secrets
// written by the previous one-map-per-environment layout. A Vault that
// already holds Cooker secrets under <prefix>/<envID> (map form) will
// not be read by this adapter, which addresses <prefix>/<envID>/<key>.
// Migrate by re-Putting each key (the handler layer re-seals on the
// next write), or run a one-off copy from the old map fields into the
// new per-key paths.
//
// Auth: token-based (VAULT_TOKEN). Operators using Vault Agent
// injector / k8s auth can keep this adapter unchanged and source the
// token via the Agent sidecar; the SDK reads VAULT_TOKEN from env at
// init when the client is constructed with a token.
package vault

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/vault-client-go"
	"github.com/hashicorp/vault-client-go/schema"
	"github.com/santapong/cooker/internal/secrets"
)

// valueField is the single KV v2 field name each per-key secret uses.
const valueField = "value"

// Manager satisfies secrets.Manager using Vault KV v2 with one secret
// per key at <mount>/<prefix>/<envID>/<key>.
type Manager struct {
	client *vault.Client
	mount  string
	prefix string
}

// Config holds the SDK-construction inputs. Address must be a full
// URL (e.g. https://vault.example.com:8200). Mount defaults to "secret".
type Config struct {
	Address string
	Token   string
	Mount   string
	Prefix  string
}

// New constructs a Manager from a config block.
//
// L vault.go: the vault-client-go SDK does not expose a per-request
// response-body cap. The SDK streams through the http.DefaultTransport
// (or whatever is configured at vault.New); adding a transport wrapper
// with an io.LimitReader on the body would require forking the SDK's
// internal HTTP layer. This is not done here: the risk is bounded by
// Vault's own server-side payload limits, and Cooker's Vault paths hold
// small single-value secrets (not blobs). If an unbounded read becomes a
// concern, configure a custom http.Transport via vault.WithHTTPClient
// before calling this constructor.
func New(cfg Config) (*Manager, error) {
	if cfg.Address == "" {
		return nil, errors.New("vault: COOKER_SECRETS_VAULT_ADDR is required")
	}
	c, err := vault.New(
		vault.WithAddress(cfg.Address),
	)
	if err != nil {
		return nil, err
	}
	if cfg.Token != "" {
		if err := c.SetToken(cfg.Token); err != nil {
			return nil, err
		}
	}
	mount := cfg.Mount
	if mount == "" {
		mount = "secret"
	}
	return &Manager{client: c, mount: mount, prefix: cfg.Prefix}, nil
}

// dirPath is the KV v2 path that holds all keys for an environment; it
// is the listing prefix, e.g. "<prefix>/<envID>".
func (m *Manager) dirPath(envID string) string {
	if m.prefix == "" {
		return envID
	}
	return m.prefix + "/" + envID
}

// keyPath is the KV v2 path of a single secret, e.g.
// "<prefix>/<envID>/<key>".
func (m *Manager) keyPath(envID, key string) string {
	return m.dirPath(envID) + "/" + key
}

func isNotFound(err error) bool {
	var rerr *vault.ResponseError
	return errors.As(err, &rerr) && rerr.StatusCode == 404
}

func (m *Manager) Get(ctx context.Context, envID, key string) ([]byte, error) {
	resp, err := m.client.Secrets.KvV2Read(ctx, m.keyPath(envID, key), vault.WithMountPath(m.mount))
	if err != nil {
		if isNotFound(err) {
			return nil, secrets.ErrNotFound
		}
		return nil, err
	}
	// A soft-deleted latest version returns 200 with a nil Data map.
	if resp == nil || resp.Data.Data == nil {
		return nil, secrets.ErrNotFound
	}
	v, ok := resp.Data.Data[valueField]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	s, ok := v.(string)
	if !ok {
		return nil, errors.New("vault: secret value is not a string")
	}
	return []byte(s), nil
}

// Put writes a single key atomically to its own path. No read-modify-
// write, so concurrent Puts of different keys in the same environment
// cannot clobber one another.
func (m *Manager) Put(ctx context.Context, envID, key string, value []byte) error {
	_, err := m.client.Secrets.KvV2Write(ctx, m.keyPath(envID, key),
		schema.KvV2WriteRequest{Data: map[string]any{valueField: string(value)}},
		vault.WithMountPath(m.mount),
	)
	return err
}

// Delete removes a single key's path entirely (metadata + all
// versions) so the key disappears from List, matching the previous
// map-field delete semantics. Deleting a missing key is a no-op.
func (m *Manager) Delete(ctx context.Context, envID, key string) error {
	_, err := m.client.Secrets.KvV2DeleteMetadataAndAllVersions(ctx, m.keyPath(envID, key), vault.WithMountPath(m.mount))
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

// List enumerates the key names under an environment's directory.
func (m *Manager) List(ctx context.Context, envID string) ([]string, error) {
	resp, err := m.client.Secrets.KvV2List(ctx, m.dirPath(envID), vault.WithMountPath(m.mount))
	if err != nil {
		if isNotFound(err) {
			return []string{}, nil
		}
		return nil, err
	}
	if resp == nil {
		return []string{}, nil
	}
	out := make([]string, 0, len(resp.Data.Keys))
	for _, k := range resp.Data.Keys {
		// KvV2List returns sub-directories with a trailing slash; a
		// per-key layout has no nested dirs, but guard anyway.
		out = append(out, strings.TrimSuffix(k, "/"))
	}
	return out, nil
}
