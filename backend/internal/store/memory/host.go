package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type hosts struct {
	mu sync.RWMutex
	m  map[string]*model.Host
}

func (s *hosts) List(_ context.Context) ([]*model.Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Host, 0, len(s.m))
	for _, h := range s.m {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *hosts) Get(_ context.Context, id string) (*model.Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("host %s: %w", id, store.ErrNotFound)
	}
	return h, nil
}

func (s *hosts) Create(_ context.Context, h *model.Host) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[h.ID] = h
	return nil
}

func (s *hosts) Update(_ context.Context, h *model.Host) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[h.ID]
	if !ok {
		return fmt.Errorf("host %s: %w", h.ID, store.ErrNotFound)
	}
	if cur.Version != h.Version {
		return fmt.Errorf("host %s: %w", h.ID, store.ErrConflict)
	}
	h.Version++
	s.m[h.ID] = h
	return nil
}

func (s *hosts) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("host %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}
