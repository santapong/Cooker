// Package memory provides an in-memory implementation of the store
// interfaces. Intended for unit tests and local development when a
// PostgreSQL instance is not available.
package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/store"
)

// New returns an in-memory aggregate store. Safe for concurrent use.
func New() *store.Store {
	return store.New(
		&pipelines{m: map[string]*model.Pipeline{}},
		&runs{m: map[string]*model.PipelineRun{}},
		&environments{m: map[string]*model.Environment{}},
		&apps{m: map[string]*model.App{}},
		&hosts{m: map[string]*model.Host{}},
		&users{byID: map[string]*model.User{}, byEmail: map[string]string{}},
		nil,
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

func (s *runs) UpdateHeartbeat(_ context.Context, id string, ts time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[id]
	if !ok {
		return fmt.Errorf("run %s: %w", id, store.ErrNotFound)
	}
	t := ts
	r.HeartbeatAt = &t
	return nil
}

func (s *runs) SweepOrphans(_ context.Context, threshold time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	swept := 0
	for _, r := range s.m {
		if r.Status != model.RunStatusRunning {
			continue
		}
		if r.HeartbeatAt == nil || now.Sub(*r.HeartbeatAt) > threshold {
			r.Status = model.RunStatusFailed
			r.Error = "orphaned: heartbeat stale at boot"
			finished := now
			r.FinishedAt = &finished
			swept++
		}
	}
	return swept, nil
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

type apps struct {
	mu sync.RWMutex
	m  map[string]*model.App
}

func (s *apps) List(_ context.Context) ([]*model.App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.App, 0, len(s.m))
	for _, a := range s.m {
		out = append(out, a)
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
	return a, nil
}

func (s *apps) GetByRepo(_ context.Context, repo, branch string) (*model.App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.m {
		if a.GitHubRepo == repo && a.Branch == branch {
			return a, nil
		}
	}
	return nil, fmt.Errorf("app %s@%s: %w", repo, branch, store.ErrNotFound)
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
	if _, ok := s.m[a.ID]; !ok {
		return fmt.Errorf("app %s: %w", a.ID, store.ErrNotFound)
	}
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
	if _, ok := s.m[h.ID]; !ok {
		return fmt.Errorf("host %s: %w", h.ID, store.ErrNotFound)
	}
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

// users is an in-memory store.UserStore. Email keys are case-insensitive
// (lowercased on the way in) so callers don't have to normalise.
type users struct {
	mu      sync.RWMutex
	byID    map[string]*model.User
	byEmail map[string]string // lowercased email → id
}

func (s *users) GetByEmail(_ context.Context, email string) (*model.User, error) {
	key := strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byEmail[key]
	if !ok {
		return nil, fmt.Errorf("user %s: %w", email, store.ErrNotFound)
	}
	return s.byID[id], nil
}

func (s *users) GetByID(_ context.Context, id string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, fmt.Errorf("user %s: %w", id, store.ErrNotFound)
	}
	return u, nil
}

func (s *users) Create(_ context.Context, u *model.User) error {
	key := strings.ToLower(strings.TrimSpace(u.Email))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[key]; exists {
		return fmt.Errorf("user %s: already exists", u.Email)
	}
	s.byID[u.ID] = u
	s.byEmail[key] = u.ID
	return nil
}

func (s *users) Update(_ context.Context, u *model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.byID[u.ID]
	if !ok {
		return fmt.Errorf("user %s: %w", u.ID, store.ErrNotFound)
	}
	if !strings.EqualFold(old.Email, u.Email) {
		delete(s.byEmail, strings.ToLower(strings.TrimSpace(old.Email)))
		s.byEmail[strings.ToLower(strings.TrimSpace(u.Email))] = u.ID
	}
	s.byID[u.ID] = u
	return nil
}

func (s *users) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID), nil
}
