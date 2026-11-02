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

type runs struct {
	mu sync.RWMutex
	m  map[string]*model.PipelineRun
}

func (s *runs) List(_ context.Context, pipelineID string, limit, offset int) ([]*model.PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	matched := make([]*model.PipelineRun, 0)
	for _, r := range s.m {
		if r.PipelineID == pipelineID {
			matched = append(matched, r)
		}
	}
	// Sort newest-first to match the Postgres ORDER BY created_at DESC.
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	if offset > 0 {
		if offset >= len(matched) {
			return []*model.PipelineRun{}, nil
		}
		matched = matched[offset:]
	}
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	// Return copies with per-stage Logs stripped, matching the Postgres
	// list projection. Stored rows must not be mutated — callers of Get
	// still see full logs.
	out := make([]*model.PipelineRun, len(matched))
	for i, r := range matched {
		cp := r.Clone()
		for j := range cp.StageRuns {
			cp.StageRuns[j].Logs = ""
		}
		out[i] = cp
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
	// Callers may continue mutating an attempt after its queue claim expires.
	// A deep copy prevents that mutation from bypassing fenced Update methods.
	return r.Clone(), nil
}

// GetSummary mirrors Get with per-stage Logs stripped (parity with the
// postgres jsonb strip).
func (s *runs) GetSummary(ctx context.Context, id string) (*model.PipelineRun, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	stripped := make([]model.StageRun, len(r.StageRuns))
	copy(stripped, r.StageRuns)
	for i := range stripped {
		stripped[i].Logs = ""
	}
	r.StageRuns = stripped
	return r, nil
}

// UpdateProgress writes only StageRuns (logs stripped), mirroring the
// postgres single-column flush.
func (s *runs) UpdateProgress(ctx context.Context, id string, stageRuns []model.StageRun) error {
	return store.GuardRunWrite(ctx, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		r, ok := s.m[id]
		if !ok {
			return fmt.Errorf("run %s: %w", id, store.ErrNotFound)
		}
		stripped := (&model.PipelineRun{StageRuns: stageRuns}).Clone().StageRuns
		for i := range stripped {
			stripped[i].Logs = ""
		}
		r.StageRuns = stripped
		return nil

	})
}

func (s *runs) Create(_ context.Context, r *model.PipelineRun) error {
	if r.Source.Validate() != nil {
		return model.ErrInvalidRunSource
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.m[r.ID]; exists {
		return store.ErrConflict
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	s.m[r.ID] = r.Clone()
	return nil
}

func (s *runs) Update(ctx context.Context, r *model.PipelineRun) error {
	return store.GuardRunWrite(ctx, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		previous, ok := s.m[r.ID]
		if !ok {
			return fmt.Errorf("run %s: %w", r.ID, store.ErrNotFound)
		}
		source, err := model.MergeRunSource(previous.Source, r.Source)
		if err != nil {
			return store.ErrConflict
		}
		cp := r.Clone()
		cp.Source = source
		s.m[r.ID] = cp
		return nil

	})
}

func (s *runs) UpdateStatus(ctx context.Context, id string, status model.RunStatus, finishedAt *time.Time, errMsg string) error {
	return store.GuardRunWrite(ctx, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		r, ok := s.m[id]
		if !ok {
			return fmt.Errorf("run %s: %w", id, store.ErrNotFound)
		}
		// Mutate only the lifecycle fields in place (mirrors UpdateHeartbeat's
		// targeted style); leaves StageRuns / HeartbeatAt untouched.
		r.Status = status
		r.FinishedAt = finishedAt
		r.Error = errMsg
		return nil

	})
}

func (s *runs) UpdateHeartbeat(ctx context.Context, id string, ts time.Time) error {
	return store.GuardRunWrite(ctx, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		r, ok := s.m[id]
		if !ok {
			return fmt.Errorf("run %s: %w", id, store.ErrNotFound)
		}
		t := ts
		r.HeartbeatAt = &t
		return nil

	})
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
