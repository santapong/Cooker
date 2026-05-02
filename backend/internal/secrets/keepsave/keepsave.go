package keepsave

import (
	"context"
	"errors"

	"github.com/cooker-ci/cooker/internal/secrets"
	"github.com/cooker-ci/cooker/internal/store"
)

// Manager is the KeepSave-backed secrets.Manager. KeepSave is the
// system of record for the configured project; Cooker neither stores
// nor caches the plaintext locally.
type Manager struct {
	client    *Client
	envs      store.EnvironmentStore
	projectID string
}

// New constructs a Manager. envs is required so the adapter can
// resolve Cooker's environment ID -> name (KeepSave addresses
// secrets by environment name).
func New(client *Client, envs store.EnvironmentStore, projectID string) *Manager {
	return &Manager{client: client, envs: envs, projectID: projectID}
}

func (m *Manager) envName(ctx context.Context, envID string) (string, error) {
	env, err := m.envs.Get(ctx, envID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", secrets.ErrNotFound
		}
		return "", err
	}
	return env.Name, nil
}

func (m *Manager) Get(ctx context.Context, envID, key string) ([]byte, error) {
	name, err := m.envName(ctx, envID)
	if err != nil {
		return nil, err
	}
	list, err := m.client.ListSecrets(ctx, m.projectID, name)
	if err != nil {
		return nil, err
	}
	for _, s := range list {
		if s.Key == key {
			return []byte(s.Value), nil
		}
	}
	return nil, secrets.ErrNotFound
}

func (m *Manager) Put(ctx context.Context, envID, key string, value []byte) error {
	name, err := m.envName(ctx, envID)
	if err != nil {
		return err
	}
	list, err := m.client.ListSecrets(ctx, m.projectID, name)
	if err != nil {
		return err
	}
	for _, s := range list {
		if s.Key == key {
			return m.client.UpdateSecret(ctx, m.projectID, s.ID, string(value))
		}
	}
	return m.client.CreateSecret(ctx, m.projectID, key, string(value), name)
}

func (m *Manager) Delete(ctx context.Context, envID, key string) error {
	name, err := m.envName(ctx, envID)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return nil
		}
		return err
	}
	list, err := m.client.ListSecrets(ctx, m.projectID, name)
	if err != nil {
		return err
	}
	for _, s := range list {
		if s.Key == key {
			return m.client.DeleteSecret(ctx, m.projectID, s.ID)
		}
	}
	return nil
}

func (m *Manager) List(ctx context.Context, envID string) ([]string, error) {
	name, err := m.envName(ctx, envID)
	if err != nil {
		return nil, err
	}
	list, err := m.client.ListSecrets(ctx, m.projectID, name)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(list))
	for _, s := range list {
		keys = append(keys, s.Key)
	}
	return keys, nil
}
