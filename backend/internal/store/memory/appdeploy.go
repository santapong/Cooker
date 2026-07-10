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

// appDeploys is the in-memory deploy-history store. Append-only,
// newest-first reads, mirroring the Postgres impl (default limit 20).
type appDeploys struct {
	mu sync.RWMutex
	m  map[string]*model.AppDeploy
}

func (s *appDeploys) Create(_ context.Context, d *model.AppDeploy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	s.m[d.ID] = d
	return nil
}

func (s *appDeploys) ListByApp(_ context.Context, appID string, limit int) ([]*model.AppDeploy, error) {
	if limit <= 0 {
		limit = 20
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.AppDeploy, 0)
	for _, d := range s.m {
		if d.AppID == appID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *appDeploys) Get(_ context.Context, id string) (*model.AppDeploy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("app deploy %s: %w", id, store.ErrNotFound)
	}
	return d, nil
}
