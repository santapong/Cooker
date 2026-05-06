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
//
// MaxBytes caps the resident set: when a Set would push us over,
// the oldest entries are evicted until we're under the cap. 0
// disables the cap (legacy behaviour). Picked at construction
// time; mutating it post-NewMemory is unsafe.
type Memory struct {
	mu       sync.Mutex
	entries  map[string]memEntry
	stopCh   chan struct{}
	maxBytes int
	bytes    int // currently held
}

type memEntry struct {
	e       Entry
	expires time.Time
}

// NewMemory builds a Memory store and starts a background sweeper
// that evicts expired entries every gcInterval. Pass a positive
// gcInterval; nil/zero disables the sweeper (test fixtures only).
// Resident-set is uncapped — see NewMemoryBounded for production.
func NewMemory(gcInterval time.Duration) *Memory {
	return NewMemoryBounded(gcInterval, 0)
}

// NewMemoryBounded builds a Memory store with a resident-set cap.
// On Set, if accepting the new entry would push past maxBytes,
// the oldest entries (by expiry timestamp) are evicted first.
// 0 disables the cap.
func NewMemoryBounded(gcInterval time.Duration, maxBytes int) *Memory {
	m := &Memory{
		entries:  make(map[string]memEntry),
		stopCh:   make(chan struct{}),
		maxBytes: maxBytes,
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
	newSize := len(key) + len(e.Body)
	if existing, ok := m.entries[key]; ok {
		m.bytes -= len(key) + len(existing.e.Body)
	}
	if m.maxBytes > 0 && m.bytes+newSize > m.maxBytes {
		m.evictUntilLocked(m.maxBytes - newSize)
	}
	m.entries[key] = memEntry{e: e, expires: time.Now().Add(ttl)}
	m.bytes += newSize
	return nil
}

// evictUntilLocked drops the oldest-expiring entries until bytes
// is below target. Caller must hold m.mu.
func (m *Memory) evictUntilLocked(target int) {
	for m.bytes > target && len(m.entries) > 0 {
		var oldestKey string
		var oldestExp time.Time
		for k, v := range m.entries {
			if oldestKey == "" || v.expires.Before(oldestExp) {
				oldestKey, oldestExp = k, v.expires
			}
		}
		ent := m.entries[oldestKey]
		m.bytes -= len(oldestKey) + len(ent.e.Body)
		delete(m.entries, oldestKey)
	}
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
					m.bytes -= len(k) + len(e.e.Body)
					delete(m.entries, k)
				}
			}
			m.mu.Unlock()
		}
	}
}
