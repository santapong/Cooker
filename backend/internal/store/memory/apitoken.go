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

// apiTokens is the in-memory APITokenStore. Tokens are keyed by id;
// GetByHash scans (the set is small in dev/test, and Postgres serves the
// hot path via its hash index in production). Mirrors the Postgres impl,
// returning copies so callers never mutate stored pointers.
type apiTokens struct {
	mu sync.RWMutex
	m  map[string]*model.APIToken
}

func cloneAPIToken(t *model.APIToken) *model.APIToken {
	cp := *t
	if t.ExpiresAt != nil {
		e := *t.ExpiresAt
		cp.ExpiresAt = &e
	}
	if t.LastUsedAt != nil {
		l := *t.LastUsedAt
		cp.LastUsedAt = &l
	}
	return &cp
}

func (s *apiTokens) Create(_ context.Context, t *model.APIToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Enforce the same hash-uniqueness the Postgres unique index gives us
	// so the two backends behave identically when a (vanishingly unlikely)
	// hash collision or a double-insert occurs.
	for _, existing := range s.m {
		if existing.Hash == t.Hash {
			return fmt.Errorf("api token hash: %w", store.ErrConflict)
		}
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	s.m[t.ID] = cloneAPIToken(t)
	return nil
}

func (s *apiTokens) ListByCreator(_ context.Context, createdBySub string) ([]*model.APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.APIToken, 0)
	for _, t := range s.m {
		if t.CreatedBySub == createdBySub {
			out = append(out, cloneAPIToken(t))
		}
	}
	sortTokensNewestFirst(out)
	return out, nil
}

func (s *apiTokens) ListAll(_ context.Context) ([]*model.APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.APIToken, 0, len(s.m))
	for _, t := range s.m {
		out = append(out, cloneAPIToken(t))
	}
	sortTokensNewestFirst(out)
	return out, nil
}

func (s *apiTokens) GetByHash(_ context.Context, hash string) (*model.APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.m {
		if t.Hash == hash {
			return cloneAPIToken(t), nil
		}
	}
	return nil, fmt.Errorf("api token by hash: %w", store.ErrNotFound)
}

func (s *apiTokens) Get(_ context.Context, id string) (*model.APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("api token %s: %w", id, store.ErrNotFound)
	}
	return cloneAPIToken(t), nil
}

func (s *apiTokens) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("api token %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}

func (s *apiTokens) TouchLastUsed(_ context.Context, id string, ts time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[id]
	if !ok {
		return fmt.Errorf("api token %s: %w", id, store.ErrNotFound)
	}
	stamp := ts
	t.LastUsedAt = &stamp
	return nil
}

// sortTokensNewestFirst orders by CreatedAt descending, breaking ties by
// id so listings are deterministic.
func sortTokensNewestFirst(ts []*model.APIToken) {
	sort.Slice(ts, func(i, j int) bool {
		if ts[i].CreatedAt.Equal(ts[j].CreatedAt) {
			return ts[i].ID < ts[j].ID
		}
		return ts[i].CreatedAt.After(ts[j].CreatedAt)
	})
}
