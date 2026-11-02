package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// registryConfigs is the in-memory RegistryConfigStore. Mirrors the
// hosts impl: name-ordered List, ErrNotFound on missing Get/Delete.
type registryConfigs struct {
	mu          sync.RWMutex
	m           map[string]*model.RegistryConfig
	nextVersion int64
}

func (s *registryConfigs) List(_ context.Context) ([]*model.RegistryConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.RegistryConfig, 0, len(s.m))
	for _, r := range s.m {
		out = append(out, r.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *registryConfigs) Get(_ context.Context, id string) (*model.RegistryConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("registry config %s: %w", id, store.ErrNotFound)
	}
	return r.Clone(), nil
}

func (s *registryConfigs) Create(_ context.Context, r *model.RegistryConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r == nil || r.ID == "" || !model.ValidRegistryIDs(r.AllowedAppIDs, 256) || !model.ValidRegistryIDs(r.AllowedPipelineIDs, 256) {
		return model.ErrInvalidRegistryAuthority
	}
	if _, exists := s.m[r.ID]; exists {
		return store.ErrConflict
	}
	for _, old := range s.m {
		if old.Name == r.Name {
			return store.ErrConflict
		}
	}
	s.nextVersion++
	r.Version = s.nextVersion
	s.m[r.ID] = r.Clone()
	return nil
}

func (s *registryConfigs) UpdateGrants(_ context.Context, id string, expectedVersion int64, appIDs, pipelineIDs []string) (*model.RegistryConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	if expectedVersion < 1 || r.Version != expectedVersion {
		return nil, store.ErrConflict
	}
	if !model.ValidRegistryIDs(appIDs, 256) || !model.ValidRegistryIDs(pipelineIDs, 256) {
		return nil, model.ErrInvalidRegistryAuthority
	}
	cp := r.Clone()
	cp.AllowedAppIDs = append([]string(nil), appIDs...)
	cp.AllowedPipelineIDs = append([]string(nil), pipelineIDs...)
	s.nextVersion++
	cp.Version = s.nextVersion
	s.m[id] = cp
	return cp.Clone(), nil
}

func (s *registryConfigs) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("registry config %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}
