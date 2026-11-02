package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type environments struct {
	mu sync.RWMutex
	m  map[string]*model.Environment
}

func (s *environments) List(ctx context.Context, limit, offset int) ([]*model.Environment, error) {
	return s.list(ctx, limit, offset, false)
}

func (s *environments) ListPublic(ctx context.Context, limit, offset int) ([]*model.Environment, error) {
	return s.list(ctx, limit, offset, true)
}

func (s *environments) list(_ context.Context, limit, offset int, public bool) ([]*model.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Environment, 0, len(s.m))
	for _, e := range s.m {
		if public && model.IsRegistryCredentialEnvironment(e) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order == out[j].Order {
			return out[i].ID < out[j].ID
		}
		return out[i].Order < out[j].Order
	})
	return paginate(out, limit, offset), nil
}

func (s *environments) Get(_ context.Context, id string) (*model.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("environment %s: %w", id, store.ErrNotFound)
	}
	return e, nil
}

func (s *environments) Create(_ context.Context, e *model.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[e.ID] = e
	return nil
}

func (s *environments) Update(_ context.Context, e *model.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[e.ID]
	if !ok {
		return fmt.Errorf("environment %s: %w", e.ID, store.ErrNotFound)
	}
	if cur.Version != e.Version {
		return fmt.Errorf("environment %s: %w", e.ID, store.ErrConflict)
	}
	e.Version++
	s.m[e.ID] = e
	return nil
}

func (s *environments) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("environment %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}
