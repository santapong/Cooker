// Package vault implements secrets.Manager backed by HashiCorp Vault's
// KV v2 secrets engine. The Cooker environment ID is mapped to a path
// segment under a configurable mount + prefix, so each environment
// becomes one Vault secret holding all its keys as fields.
//
// Auth: token-based (VAULT_TOKEN). Operators using Vault Agent
// injector / k8s auth can keep this adapter unchanged and source the
// token via the Agent sidecar; the SDK reads VAULT_TOKEN from env at
// init when the client is constructed with a token.
package vault

import (
	"context"
	"errors"

	"github.com/santapong/cooker/internal/secrets"
	"github.com/hashicorp/vault-client-go"
	"github.com/hashicorp/vault-client-go/schema"
)

// Manager satisfies secrets.Manager using Vault KV v2 under
// <mount>/<prefix>/<envID> with one field per Cooker secret key.
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

func (m *Manager) path(envID string) string {
	if m.prefix == "" {
		return envID
	}
	return m.prefix + "/" + envID
}

func (m *Manager) read(ctx context.Context, envID string) (map[string]any, error) {
	resp, err := m.client.Secrets.KvV2Read(ctx, m.path(envID), vault.WithMountPath(m.mount))
	if err != nil {
		var rerr *vault.ResponseError
		if errors.As(err, &rerr) && rerr.StatusCode == 404 {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if resp == nil || resp.Data.Data == nil {
		return map[string]any{}, nil
	}
	return resp.Data.Data, nil
}

func (m *Manager) write(ctx context.Context, envID string, data map[string]any) error {
	_, err := m.client.Secrets.KvV2Write(ctx, m.path(envID),
		schema.KvV2WriteRequest{Data: data},
		vault.WithMountPath(m.mount),
	)
	return err
}

func (m *Manager) Get(ctx context.Context, envID, key string) ([]byte, error) {
	data, err := m.read(ctx, envID)
	if err != nil {
		return nil, err
	}
	v, ok := data[key]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	s, ok := v.(string)
	if !ok {
		return nil, errors.New("vault: secret value is not a string")
	}
	return []byte(s), nil
}

func (m *Manager) Put(ctx context.Context, envID, key string, value []byte) error {
	data, err := m.read(ctx, envID)
	if err != nil {
		return err
	}
	data[key] = string(value)
	return m.write(ctx, envID, data)
}

func (m *Manager) Delete(ctx context.Context, envID, key string) error {
	data, err := m.read(ctx, envID)
	if err != nil {
		return err
	}
	if _, ok := data[key]; !ok {
		return nil
	}
	delete(data, key)
	return m.write(ctx, envID, data)
}

func (m *Manager) List(ctx context.Context, envID string) ([]string, error) {
	data, err := m.read(ctx, envID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(data))
	for k := range data {
		out = append(out, k)
	}
	return out, nil
}
