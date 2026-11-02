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

	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
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

// EnvironmentGenerationBound identifies the local backend whose writes advance
// Environment.Version. It does not claim that old encrypted values are retained.
func (m *Manager) EnvironmentGenerationBound() bool { return true }

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
	env, err := m.editSnapshot(ctx, envID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return secrets.ErrNotFound
		}
		return err
	}
	_, err = m.putSnapshot(ctx, env, key, value)
	return err
}

func (m *Manager) Delete(ctx context.Context, envID, key string) error {
	env, err := m.editSnapshot(ctx, envID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	delete(env.Secrets, key)
	return m.envs.Update(ctx, env)
}

// editSnapshot detaches mutable state before any edit. In-memory/custom stores
// can return shared pointers; mutating those before Update would bypass CAS on
// a failed edit. The value slices are immutable in this operation.
func (m *Manager) editSnapshot(ctx context.Context, envID string) (*model.Environment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	env, err := m.envs.Get(ctx, envID)
	if err != nil {
		return nil, err
	}
	cp := *env
	cp.Secrets = make(map[string][]byte, len(env.Secrets))
	for k, v := range env.Secrets {
		cp.Secrets[k] = v
	}
	return &cp, nil
}

func (m *Manager) putSnapshot(ctx context.Context, env *model.Environment, key string, value []byte) (int, error) {
	if _, exists := env.PlainVars[key]; exists {
		return 0, secrets.ErrKeyCollision
	}
	// Honour the deprecated alias in legacy stored records as well.
	if _, exists := env.Variables[key]; exists {
		return 0, secrets.ErrKeyCollision
	}
	sealed, err := m.codec.Seal(value)
	if err != nil {
		return 0, fmt.Errorf("seal: %w", err)
	}
	env.Secrets[key] = sealed
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := m.envs.Update(ctx, env); err != nil {
		return 0, err
	}
	return env.Version, nil
}

func (m *Manager) conditionalSnapshot(ctx context.Context, envID string, generation int) (*model.Environment, error) {
	if generation < 0 {
		return nil, secrets.ErrGenerationConflict
	}
	env, err := m.editSnapshot(ctx, envID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, secrets.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if env.Version != generation {
		return nil, secrets.ErrGenerationConflict
	}
	return env, nil
}

func conditionalError(err error) error {
	if errors.Is(err, store.ErrConflict) {
		return secrets.ErrGenerationConflict
	}
	if errors.Is(err, store.ErrNotFound) {
		return secrets.ErrNotFound
	}
	return err
}

// PutAtGeneration atomically fences configuration and secret changes through
// the existing EnvironmentStore.Update CAS; it never retries with newer data.
func (m *Manager) PutAtGeneration(ctx context.Context, envID, key string, value []byte, generation int) (int, error) {
	env, err := m.conditionalSnapshot(ctx, envID, generation)
	if err != nil {
		return 0, err
	}
	next, err := m.putSnapshot(ctx, env, key, value)
	return next, conditionalError(err)
}

func (m *Manager) DeleteAtGeneration(ctx context.Context, envID, key string, generation int) (int, error) {
	env, err := m.conditionalSnapshot(ctx, envID, generation)
	if err != nil {
		return 0, err
	}
	delete(env.Secrets, key)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := m.envs.Update(ctx, env); err != nil {
		return 0, conditionalError(err)
	}
	return env.Version, nil
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
