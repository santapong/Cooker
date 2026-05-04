// Package idempotency caches mutating-request results keyed by an
// Idempotency-Key (RFC-draft) or X-GitHub-Delivery header so that
// webhook retries and double-clicked Run buttons don't spawn
// duplicate work.
//
// The in-memory implementation is per-replica with TTL eviction;
// multi-replica deployments should swap in a Redis-backed store
// (follow-up; the interface is the same).
package idempotency

import (
	"context"
	"sync"
	"time"
)

// Entry is what we cache: the HTTP status and body of the original
// successful response, so duplicate requests can replay the exact
// answer the first one got.
type Entry struct {
	Status int
	Body   []byte
}

// Store is the dedup interface. Get returns (entry, true) on hit;
// Set stores a result with a TTL.
type Store interface {
	Get(ctx context.Context, key string) (Entry, bool)
	Set(ctx context.Context, key string, e Entry, ttl time.Duration) error
}

// Memory is a per-process Store. It's adequate for single-replica
// deployments and as a fallback when no Redis URL is configured.
type Memory struct {
	mu      sync.Mutex
	entries map[string]memEntry
	stopCh  chan struct{}
}

type memEntry struct {
	e       Entry
	expires time.Time
}

// NewMemory builds a Memory store and starts a background sweeper
// that evicts expired entries every gcInterval. Pass a positive
// gcInterval; nil/zero disables the sweeper (test fixtures only).
func NewMemory(gcInterval time.Duration) *Memory {
	m := &Memory{
		entries: make(map[string]memEntry),
		stopCh:  make(chan struct{}),
	}
	if gcInterval > 0 {
		go m.gc(gcInterval)
	}
	return m
}

// Close stops the background sweeper. Safe to call multiple times.
func (m *Memory) Close() {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
}

func (m *Memory) Get(_ context.Context, key string) (Entry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entries[key]; ok && time.Now().Before(e.expires) {
		return e.e, true
	}
	return Entry{}, false
}

func (m *Memory) Set(_ context.Context, key string, e Entry, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = memEntry{e: e, expires: time.Now().Add(ttl)}
	return nil
}

func (m *Memory) gc(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case now := <-t.C:
			m.mu.Lock()
			for k, e := range m.entries {
				if now.After(e.expires) {
					delete(m.entries, k)
				}
			}
			m.mu.Unlock()
		}
	}
}
