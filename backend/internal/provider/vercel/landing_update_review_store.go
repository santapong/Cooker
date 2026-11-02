package vercel

import (
	"context"
	"sort"
	"sync"
	"time"
)

// LandingUpdateReviewStore supports immutable review preparation and reads only.
// List returns the most recent 100 receipts; Get preserves older review access.
// Create must atomically recheck the actual connection and adoption records.
type LandingUpdateReviewStore interface {
	Create(context.Context, *LandingUpdateReview) error
	Get(context.Context, string) (*LandingUpdateReview, error)
	List(context.Context) ([]*LandingUpdateReview, error)
}

type MemoryLandingUpdateReviews struct {
	connections *Memory
	adoptions   *MemoryLandingAdoptions
	mu          sync.Mutex
	reviews     map[string]*LandingUpdateReview
}

func NewMemoryLandingUpdateReviews(connections *Memory, adoptions *MemoryLandingAdoptions) *MemoryLandingUpdateReviews {
	return &MemoryLandingUpdateReviews{connections: connections, adoptions: adoptions, reviews: map[string]*LandingUpdateReview{}}
}

func (m *MemoryLandingUpdateReviews) Create(ctx context.Context, r *LandingUpdateReview) error {
	if m == nil || m.connections == nil || m.adoptions == nil || m.adoptions.connections != m.connections {
		return ErrUnavailable
	}
	if err := validateLandingUpdateReview(r); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Same lock order as adoption writes; no provider work occurs under locks.
	m.connections.mu.Lock()
	defer m.connections.mu.Unlock()
	m.adoptions.mu.Lock()
	defer m.adoptions.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !r.ExpiresAt.After(time.Now().UTC()) || r.CreatedAt.After(time.Now().UTC().Add(time.Microsecond)) {
		return ErrConflict
	}
	a := m.adoptions.records[r.AdoptionID]
	if !reviewAdoptionMatches(a, r) || !adoptionConnectionMatches(m.connections.records[r.ConnectionID], a) {
		return ErrConflict
	}
	if _, exists := m.reviews[r.ID]; exists {
		return ErrConflict
	}
	m.reviews[r.ID] = cloneLandingUpdateReview(r)
	return nil
}

func (m *MemoryLandingUpdateReviews) Get(ctx context.Context, id string) (*LandingUpdateReview, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	r, ok := m.reviews[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneLandingUpdateReview(r), nil
}

func (m *MemoryLandingUpdateReviews) List(ctx context.Context) ([]*LandingUpdateReview, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	out := make([]*LandingUpdateReview, 0, len(m.reviews))
	for _, r := range m.reviews {
		out = append(out, cloneLandingUpdateReview(r))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > LandingUpdateReviewHistoryLimit {
		out = out[:LandingUpdateReviewHistoryLimit]
	}
	return out, nil
}
