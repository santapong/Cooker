package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// auditEvents is the in-memory audit trail: a bounded ring (~10k)
// for dev parity with the Postgres impl. Older events fall off the
// front; nothing here survives a restart.
type auditEvents struct {
	mu     sync.RWMutex
	events []*model.AuditEvent
	nextID int64
}

const auditRingMax = 10_000

func (s *auditEvents) Insert(_ context.Context, e *model.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	e.ID = s.nextID
	s.events = append(s.events, e)
	if len(s.events) > auditRingMax {
		s.events = s.events[len(s.events)-auditRingMax:]
	}
	return nil
}

func (s *auditEvents) Query(_ context.Context, q store.AuditQuery) ([]*model.AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	matched := make([]*model.AuditEvent, 0)
	// Newest first: walk the ring backwards.
	for i := len(s.events) - 1; i >= 0; i-- {
		e := s.events[i]
		if q.From != nil && e.Time.Before(*q.From) {
			continue
		}
		if q.To != nil && e.Time.After(*q.To) {
			continue
		}
		if q.UserSub != "" && e.UserSub != q.UserSub {
			continue
		}
		if q.Method != "" && e.Method != q.Method {
			continue
		}
		if q.PathPrefix != "" && !strings.HasPrefix(e.Path, q.PathPrefix) {
			continue
		}
		matched = append(matched, e)
	}
	if offset >= len(matched) {
		return []*model.AuditEvent{}, nil
	}
	matched = matched[offset:]
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (s *auditEvents) DeleteOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Collect kept events into a fresh backing array so the old array
	// (up to ~5 MB at auditRingMax) can be GC'd instead of being pinned
	// by the reslice-to-zero trick (DA-M / memory.go:783 fix).
	kept := make([]*model.AuditEvent, 0, len(s.events))
	deleted := 0
	for _, e := range s.events {
		if e.Time.Before(cutoff) {
			deleted++
			continue
		}
		kept = append(kept, e)
	}
	s.events = kept
	return deleted, nil
}
