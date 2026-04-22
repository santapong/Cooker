// Package memory provides an in-memory implementation of the store
// interfaces. Intended for unit tests and local development when a
// PostgreSQL instance is not available.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/store"
)

// New returns an in-memory aggregate store. Safe for concurrent use.
func New() *store.Store {
	return store.New(
		&pipelines{m: map[string]*model.Pipeline{}},
		&runs{m: map[string]*model.PipelineRun{}},
		&environments{m: map[string]*model.Environment{}},
		nil,
	)
}

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
	if _, ok := s.m[p.ID]; !ok {
		return fmt.Errorf("pipeline %s: %w", p.ID, store.ErrNotFound)
	}
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

type runs struct {
	mu sync.RWMutex
	m  map[string]*model.PipelineRun
}

func (s *runs) List(_ context.Context, pipelineID string) ([]*model.PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.PipelineRun, 0)
	for _, r := range s.m {
		if r.PipelineID == pipelineID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *runs) Get(_ context.Context, id string) (*model.PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("run %s: %w", id, store.ErrNotFound)
	}
	return r, nil
}

func (s *runs) Create(_ context.Context, r *model.PipelineRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[r.ID] = r
	return nil
}

func (s *runs) Update(_ context.Context, r *model.PipelineRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[r.ID]; !ok {
		return fmt.Errorf("run %s: %w", r.ID, store.ErrNotFound)
	}
	s.m[r.ID] = r
	return nil
}

type environments struct {
	mu sync.RWMutex
	m  map[string]*model.Environment
}

func (s *environments) List(_ context.Context) ([]*model.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Environment, 0, len(s.m))
	for _, e := range s.m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
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
	if _, ok := s.m[e.ID]; !ok {
		return fmt.Errorf("environment %s: %w", e.ID, store.ErrNotFound)
	}
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
