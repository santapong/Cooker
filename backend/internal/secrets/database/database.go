// Package database is the default secrets backend. It encrypts each
// value with AES-GCM via crypto.Codec and persists the ciphertext on
// the Environment row's Secrets map. Behavior matches pre-Manager
// Cooker; this package only extracts the logic so a Manager-aware
// handler can delegate to either this or another adapter.
package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/cooker-ci/cooker/internal/crypto"
	"github.com/cooker-ci/cooker/internal/secrets"
	"github.com/cooker-ci/cooker/internal/store"
)

// Manager is the database-backed secrets.Manager.
type Manager struct {
	envs  store.EnvironmentStore
	codec *crypto.Codec
}

// New returns a Manager bound to the given environment store and
// codec. Both are required; the caller (server boot) is responsible
// for not constructing a Manager when the codec is inactive.
func New(envs store.EnvironmentStore, codec *crypto.Codec) *Manager {
	return &Manager{envs: envs, codec: codec}
}

func (m *Manager) Get(ctx context.Context, envID, key string) ([]byte, error) {
	env, err := m.envs.Get(ctx, envID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, secrets.ErrNotFound
		}
		return nil, err
	}
	sealed, ok := env.Secrets[key]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	plain, err := m.codec.Open(sealed)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return plain, nil
}

func (m *Manager) Put(ctx context.Context, envID, key string, value []byte) error {
	env, err := m.envs.Get(ctx, envID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return secrets.ErrNotFound
		}
		return err
	}
	sealed, err := m.codec.Seal(value)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	if env.Secrets == nil {
		env.Secrets = map[string][]byte{}
	}
	env.Secrets[key] = sealed
	return m.envs.Update(ctx, env)
}

func (m *Manager) Delete(ctx context.Context, envID, key string) error {
	env, err := m.envs.Get(ctx, envID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	delete(env.Secrets, key)
	return m.envs.Update(ctx, env)
}

func (m *Manager) List(ctx context.Context, envID string) ([]string, error) {
	env, err := m.envs.Get(ctx, envID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, secrets.ErrNotFound
		}
		return nil, err
	}
	keys := make([]string, 0, len(env.Secrets))
	for k := range env.Secrets {
		keys = append(keys, k)
	}
	return keys, nil
}
