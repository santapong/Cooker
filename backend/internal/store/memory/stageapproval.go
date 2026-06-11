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

// stageApprovals is the in-memory StageApprovalStore. Gates are keyed by
// id; votes live in a side map keyed by gate id. Mirrors the Postgres
// impl, including the (run, stage) uniqueness on create and the
// one-vote-per-approver dedupe on AddVote.
type stageApprovals struct {
	mu    sync.RWMutex
	m     map[string]*model.StageApproval
	votes map[string][]model.StageApprovalVote
}

// findByRunStageLocked returns the gate for (runID, stageID). Caller must
// hold at least a read lock.
func (s *stageApprovals) findByRunStageLocked(runID, stageID string) *model.StageApproval {
	for _, g := range s.m {
		if g.RunID == runID && g.StageID == stageID {
			return g
		}
	}
	return nil
}

// hydrate returns a copy of g with its Votes slice filled from the side
// map, so callers never see the stored pointer or a shared votes slice.
func (s *stageApprovals) hydrate(g *model.StageApproval) *model.StageApproval {
	cp := *g
	stored := s.votes[g.ID]
	cp.Votes = make([]model.StageApprovalVote, len(stored))
	copy(cp.Votes, stored)
	return &cp
}

func (s *stageApprovals) CreateGate(_ context.Context, g *model.StageApproval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findByRunStageLocked(g.RunID, g.StageID) != nil {
		return fmt.Errorf("stage approval %s/%s: %w", g.RunID, g.StageID, store.ErrConflict)
	}
	now := time.Now()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
	}
	g.UpdatedAt = g.CreatedAt
	cp := *g
	cp.Votes = nil
	s.m[g.ID] = &cp
	return nil
}

func (s *stageApprovals) GetGate(_ context.Context, runID, stageID string) (*model.StageApproval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g := s.findByRunStageLocked(runID, stageID)
	if g == nil {
		return nil, fmt.Errorf("stage approval %s/%s: %w", runID, stageID, store.ErrNotFound)
	}
	return s.hydrate(g), nil
}

func (s *stageApprovals) ListGates(_ context.Context, runID string) ([]*model.StageApproval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.StageApproval, 0)
	for _, g := range s.m {
		if g.RunID == runID {
			out = append(out, s.hydrate(g))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *stageApprovals) UpdateGateStatus(_ context.Context, id string, status model.StageApprovalStatus, resolvedBy string, resolvedAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.m[id]
	if !ok {
		return fmt.Errorf("stage approval %s: %w", id, store.ErrNotFound)
	}
	g.Status = status
	if resolvedBy != "" {
		g.ResolvedBy = resolvedBy
	}
	if resolvedAt != nil {
		t := *resolvedAt
		g.ResolvedAt = &t
	}
	g.UpdatedAt = time.Now()
	return nil
}

func (s *stageApprovals) AddVote(_ context.Context, v *model.StageApprovalVote) (bool, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[v.StageApprovalID]; !ok {
		return false, 0, fmt.Errorf("stage approval %s: %w", v.StageApprovalID, store.ErrNotFound)
	}
	existing := s.votes[v.StageApprovalID]
	for i := range existing {
		if existing[i].ApproverSub == v.ApproverSub {
			// Idempotent: same approver, no new row, count unchanged.
			return false, len(existing), nil
		}
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now()
	}
	s.votes[v.StageApprovalID] = append(existing, *v)
	return true, len(existing) + 1, nil
}
