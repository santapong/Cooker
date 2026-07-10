package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// clusterConfigs is the in-memory ClusterConfigStore. Same shape as
// registryConfigs.
type clusterConfigs struct {
	mu sync.RWMutex
	m  map[string]*model.ClusterConfig
}

func (s *clusterConfigs) List(_ context.Context) ([]*model.ClusterConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.ClusterConfig, 0, len(s.m))
	for _, c := range s.m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *clusterConfigs) Get(_ context.Context, id string) (*model.ClusterConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("cluster config %s: %w", id, store.ErrNotFound)
	}
	return c, nil
}

func (s *clusterConfigs) Create(_ context.Context, c *model.ClusterConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[c.ID] = c
	return nil
}

func (s *clusterConfigs) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("cluster config %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}
