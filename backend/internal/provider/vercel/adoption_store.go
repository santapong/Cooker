package vercel

import (
	"context"
	"sort"
	"sync"
)

// LandingAdoptionStore must check the authoritative connection in the same
// critical section as an enabled create/reauthorization. Checking it only in
// the service would race selection, token replacement, pause or disconnect.
type LandingAdoptionStore interface {
	Get(context.Context, string) (*LandingAdoption, error)
	List(context.Context) ([]*LandingAdoption, error)
	Create(context.Context, *LandingAdoption) error
	Update(context.Context, *LandingAdoption, int) error
}

const maxLandingAdoptions = 1000

type MemoryLandingAdoptions struct {
	connections *Memory
	mu          sync.Mutex
	records     map[string]*LandingAdoption
}

// NewMemoryLandingAdoptions shares the connection store's actual lock. It is a
// fixture/development adapter, not a cross-process persistence guarantee.
func NewMemoryLandingAdoptions(connections *Memory) *MemoryLandingAdoptions {
	return &MemoryLandingAdoptions{connections: connections, records: map[string]*LandingAdoption{}}
}

func (m *MemoryLandingAdoptions) Get(_ context.Context, id string) (*LandingAdoption, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneLandingAdoption(a), nil
}

func (m *MemoryLandingAdoptions) List(_ context.Context) ([]*LandingAdoption, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.records) > maxLandingAdoptions {
		return nil, limitError("landing adoption inventory exceeds the supported limit")
	}
	out := []*LandingAdoption{}
	for _, a := range m.records {
		out = append(out, cloneLandingAdoption(a))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (m *MemoryLandingAdoptions) Create(_ context.Context, a *LandingAdoption) error {
	if m == nil || m.connections == nil {
		return ErrUnavailable
	}
	if err := validateLandingAdoption(a); err != nil {
		return err
	}
	if a.Version != 1 || !a.Enabled {
		return ErrInvalid
	}
	m.connections.mu.Lock()
	defer m.connections.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !adoptionConnectionMatches(m.connections.records[a.ConnectionID], a) {
		return ErrConflict
	}
	if _, ok := m.records[a.ID]; ok {
		return ErrConflict
	}
	for _, existing := range m.records {
		if existing.Enabled && sameAdoptionDestination(existing, a) {
			return ErrConflict
		}
	}
	m.records[a.ID] = cloneLandingAdoption(a)
	return nil
}

func (m *MemoryLandingAdoptions) Update(_ context.Context, a *LandingAdoption, version int) error {
	if m == nil || m.connections == nil {
		return ErrUnavailable
	}
	if err := validateLandingAdoption(a); err != nil {
		return err
	}
	if a.Version != version {
		return ErrConflict
	}
	m.connections.mu.Lock()
	defer m.connections.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.records[a.ID]
	if !ok {
		return ErrNotFound
	}
	if old.Version != version || !sameAdoptionIdentity(old, a) || (a.Enabled && !adoptionConnectionMatches(m.connections.records[a.ConnectionID], a)) {
		return ErrConflict
	}
	for _, other := range m.records {
		if a.Enabled && other.ID != a.ID && other.Enabled && sameAdoptionDestination(other, a) {
			return ErrConflict
		}
	}
	next := cloneLandingAdoption(a)
	next.Version++
	m.records[a.ID] = next
	a.Version = next.Version
	return nil
}
