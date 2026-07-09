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

// promotions is the in-memory PromotionStore. Promotions are keyed by
// id; approvals live in a side map keyed by promotion id. Mirrors the
// Postgres impl, including the (run, environment) uniqueness on create
// and the one-approval-per-approver dedupe on AddApproval.
type promotions struct {
	mu        sync.RWMutex
	m         map[string]*model.RunPromotion
	approvals map[string][]model.PromotionApproval
}

// findByRunEnvLocked returns the promotion for (runID, environmentID).
// Caller must hold at least a read lock.
func (s *promotions) findByRunEnvLocked(runID, environmentID string) *model.RunPromotion {
	for _, p := range s.m {
		if p.RunID == runID && p.EnvironmentID == environmentID {
			return p
		}
	}
	return nil
}

// hydrate returns a deep-ish copy of p with its Approvals slice filled
// from the side map, so callers never see the stored pointer or a
// shared approvals slice.
func (s *promotions) hydrate(p *model.RunPromotion) *model.RunPromotion {
	cp := *p
	stored := s.approvals[p.ID]
	cp.Approvals = make([]model.PromotionApproval, len(stored))
	copy(cp.Approvals, stored)
	return &cp
}

func (s *promotions) CreatePromotion(_ context.Context, p *model.RunPromotion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findByRunEnvLocked(p.RunID, p.EnvironmentID) != nil {
		return fmt.Errorf("promotion %s/%s: %w", p.RunID, p.EnvironmentID, store.ErrConflict)
	}
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = p.CreatedAt
	cp := *p
	cp.Approvals = nil
	s.m[p.ID] = &cp
	return nil
}

func (s *promotions) GetPromotion(_ context.Context, runID, environmentID string) (*model.RunPromotion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.findByRunEnvLocked(runID, environmentID)
	if p == nil {
		return nil, fmt.Errorf("promotion %s/%s: %w", runID, environmentID, store.ErrNotFound)
	}
	return s.hydrate(p), nil
}

func (s *promotions) ListPromotions(_ context.Context, runID string) ([]*model.RunPromotion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.RunPromotion, 0)
	for _, p := range s.m {
		if p.RunID == runID {
			out = append(out, s.hydrate(p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *promotions) UpdatePromotionStatus(_ context.Context, id string, status model.PromotionStatus, promotedAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[id]
	if !ok {
		return fmt.Errorf("promotion %s: %w", id, store.ErrNotFound)
	}
	p.Status = status
	if promotedAt != nil {
		t := *promotedAt
		p.PromotedAt = &t
	}
	p.UpdatedAt = time.Now()
	return nil
}

func (s *promotions) AddApproval(_ context.Context, a *model.PromotionApproval) (bool, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[a.PromotionID]; !ok {
		return false, 0, fmt.Errorf("promotion %s: %w", a.PromotionID, store.ErrNotFound)
	}
	existing := s.approvals[a.PromotionID]
	for i := range existing {
		if existing[i].ApproverSub == a.ApproverSub {
			// Idempotent: same approver, no new row, count unchanged.
			return false, len(existing), nil
		}
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	s.approvals[a.PromotionID] = append(existing, *a)
	return true, len(existing) + 1, nil
}
