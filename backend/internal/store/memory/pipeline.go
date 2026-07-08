package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type pipelines struct {
	mu sync.RWMutex
	m  map[string]*model.Pipeline
}

func (s *pipelines) List(_ context.Context) ([]*model.Pipeline, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Pipeline, 0, len(s.m))
	for _, p := range s.m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *pipelines) Get(_ context.Context, id string) (*model.Pipeline, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("pipeline %s: %w", id, store.ErrNotFound)
	}
	return p, nil
}

func (s *pipelines) Create(_ context.Context, p *model.Pipeline) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[p.ID] = p
	return nil
}

func (s *pipelines) Update(_ context.Context, p *model.Pipeline) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[p.ID]
	if !ok {
		return fmt.Errorf("pipeline %s: %w", p.ID, store.ErrNotFound)
	}
	if cur.Version != p.Version {
		return fmt.Errorf("pipeline %s: %w", p.ID, store.ErrConflict)
	}
	p.Version++
	s.m[p.ID] = p
	return nil
}

func (s *pipelines) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("pipeline %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}
