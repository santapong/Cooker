package memory

import (
	"context"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// activeLicenseID is the fixed sentinel primary key for the single
// installed license. It mirrors the Postgres impl's constant and the
// licenses_one_row CHECK (migration 024): there is at most one license
// per instance, so Set always normalises the id to this value rather
// than persisting whatever the caller passed.
const activeLicenseID = "active"

// licenses is the in-memory LicenseStore: a single guarded pointer, since
// there is at most one installed license per instance. Mirrors the
// Postgres single-row impl — Set replaces (normalising the id to the
// "active" sentinel), GetActive returns ErrNotFound when empty, Delete
// clears (and is a no-op when already empty). Returns copies so callers
// never mutate the stored record.
type licenses struct {
	mu      sync.RWMutex
	current *model.License
}

func cloneLicense(l *model.License) *model.License {
	cp := *l
	if l.ExpiresAt != nil {
		e := *l.ExpiresAt
		cp.ExpiresAt = &e
	}
	if l.Features != nil {
		f := make([]string, len(l.Features))
		copy(f, l.Features)
		cp.Features = f
	}
	return &cp
}

func (s *licenses) GetActive(_ context.Context) (*model.License, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return nil, store.ErrNotFound
	}
	return cloneLicense(s.current), nil
}

func (s *licenses) Set(_ context.Context, l *model.License) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.InstalledAt.IsZero() {
		l.InstalledAt = time.Now()
	}
	// Single active license: normalise the id to the fixed sentinel so the
	// stored row matches Postgres (which keys every write on 'active' and
	// enforces it with the licenses_one_row CHECK). Mutate the caller's
	// pointer too, so callers that reuse it observe the same id across both
	// backends — without this, memory leaked the service's UUID while
	// Postgres returned "active" for the identical code path.
	l.ID = activeLicenseID
	s.current = cloneLicense(l)
	return nil
}

func (s *licenses) Delete(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Clearing an already-empty license is a no-op (no error): "remove
	// the installed license" is idempotent.
	s.current = nil
	return nil
}
