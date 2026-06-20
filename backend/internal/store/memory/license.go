package memory

import (
	"context"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// licenses is the in-memory LicenseStore: a single guarded pointer, since
// there is at most one installed license per instance. Mirrors the
// Postgres single-row impl — Set replaces, GetActive returns ErrNotFound
// when empty, Delete clears (and is a no-op when already empty). Returns
// copies so callers never mutate the stored record.
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
