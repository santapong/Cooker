package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type apps struct {
	mu sync.RWMutex
	m  map[string]*model.App
}

func (s *apps) List(_ context.Context) ([]*model.App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.App, 0, len(s.m))
	for _, a := range s.m {
		out = append(out, normalizeAppCanary(a))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *apps) Get(_ context.Context, id string) (*model.App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	return normalizeAppCanary(a), nil
}

func (s *apps) GetByRepo(_ context.Context, repo, branch string) (*model.App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.m {
		if a.GitHubRepo == repo && a.Branch == branch {
			return normalizeAppCanary(a), nil
		}
	}
	return nil, fmt.Errorf("app %s@%s: %w", repo, branch, store.ErrNotFound)
}

// normalizeAppCanary returns a (shallow-copied) App with its canary
// config normalised, so a stored-empty config reads back as an explicit
// rolling default — matching the Postgres scanApp path. The copy keeps
// the caller from mutating the stored pointer's CanaryConfig.
func normalizeAppCanary(a *model.App) *model.App {
	cp := *a
	cp.Canary = a.Canary.Normalize()
	return &cp
}

func (s *apps) Create(_ context.Context, a *model.App) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[a.ID] = a
	return nil
}

func (s *apps) Update(_ context.Context, a *model.App) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[a.ID]
	if !ok {
		return fmt.Errorf("app %s: %w", a.ID, store.ErrNotFound)
	}
	if cur.Version != a.Version {
		return fmt.Errorf("app %s: %w", a.ID, store.ErrConflict)
	}
	a.Version++
	s.m[a.ID] = a
	return nil
}

func (s *apps) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}

// UpdateHealth writes the latest probe verdict in place. Does NOT
// bump Version — health is observational state, not user-visible
// configuration, so a probe write should never block a concurrent
// Update via optimistic-concurrency conflict.
// deployedURL may be empty; an empty string leaves the existing value intact.
func (s *apps) UpdateHealth(_ context.Context, id string, status model.AppHealth, msg string, at time.Time, deployedURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[id]
	if !ok {
		return fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	cur.HealthStatus = status
	cur.HealthMessage = msg
	tt := at
	cur.HealthCheckedAt = &tt
	if deployedURL != "" {
		cur.DeployedURL = deployedURL
	}
	return nil
}

func (s *apps) UpdateDeployedURL(_ context.Context, id, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[id]
	if !ok {
		return fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	cur.DeployedURL = url
	return nil
}
