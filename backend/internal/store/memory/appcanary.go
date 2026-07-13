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

// appCanaries is the in-memory canary-state store (OR-1). Mirrors the
// Postgres impl: at most one progressing canary per app (Create rejects
// a second with ErrConflict), newest-first history.
type appCanaries struct {
	mu sync.RWMutex
	m  map[string]*model.AppCanary
}

// canaryActive reports whether a status occupies the one-per-app slot
// guarded by the partial unique index (migration 026): pending or
// progressing. Terminal rows don't count.
func canaryActive(st model.CanaryStatus) bool {
	return st == model.CanaryPending || st == model.CanaryProgressing
}

func (s *appCanaries) Create(_ context.Context, c *model.AppCanary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if canaryActive(c.Status) {
		for _, existing := range s.m {
			if existing.AppID == c.AppID && canaryActive(existing.Status) {
				return fmt.Errorf("app %s: canary in flight: %w", c.AppID, store.ErrConflict)
			}
		}
	}
	if c.StartedAt.IsZero() {
		c.StartedAt = time.Now()
	}
	c.UpdatedAt = c.StartedAt
	cp := *c
	s.m[c.ID] = &cp
	return nil
}

func (s *appCanaries) Get(_ context.Context, id string) (*model.AppCanary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("app canary %s: %w", id, store.ErrNotFound)
	}
	cp := *c
	return &cp, nil
}

func (s *appCanaries) GetActive(_ context.Context, appID string) (*model.AppCanary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.m {
		if c.AppID == appID && c.Status == model.CanaryProgressing {
			cp := *c
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("app %s: no active canary: %w", appID, store.ErrNotFound)
}

func (s *appCanaries) DeleteStalePending(_ context.Context, olderThan time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, c := range s.m {
		if c.Status == model.CanaryPending && c.StartedAt.Before(olderThan) {
			delete(s.m, id)
			n++
		}
	}
	return n, nil
}

func (s *appCanaries) ClaimTerminal(_ context.Context, id string, to model.CanaryStatus) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[id]
	if !ok {
		return false, fmt.Errorf("app canary %s: %w", id, store.ErrNotFound)
	}
	if c.Status != model.CanaryProgressing {
		return false, nil // already resolved by a concurrent actor
	}
	c.Status = to
	c.UpdatedAt = time.Now()
	return true, nil
}

func (s *appCanaries) LatestPromoted(_ context.Context, appID string) (*model.AppCanary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *model.AppCanary
	for _, c := range s.m {
		if c.AppID != appID || c.Status != model.CanaryPromoted || c.ResolvedAt == nil {
			continue
		}
		if latest == nil || c.ResolvedAt.After(*latest.ResolvedAt) {
			latest = c
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("app %s: no promoted canary: %w", appID, store.ErrNotFound)
	}
	cp := *latest
	return &cp, nil
}

func (s *appCanaries) Update(_ context.Context, c *model.AppCanary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[c.ID]; !ok {
		return fmt.Errorf("app canary %s: %w", c.ID, store.ErrNotFound)
	}
	c.UpdatedAt = time.Now()
	cp := *c
	s.m[c.ID] = &cp
	return nil
}

func (s *appCanaries) ListProgressing(_ context.Context) ([]*model.AppCanary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.AppCanary, 0)
	for _, c := range s.m {
		if c.Status == model.CanaryProgressing {
			cp := *c
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}
