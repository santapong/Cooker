package logstore

import (
	"container/list"
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync"
)

// Memory is the default, single-replica log store: a bounded per-stage
// ring buffer plus an LRU over streams. It holds no durable state and is
// lost on restart — the canonical post-run history remains StageRun.Logs.
//
// Two byte/stream caps bound memory: each stream drops its oldest entries
// once it exceeds maxBytesPerStage, and the whole least-recently-appended
// stream is evicted once the live stream count exceeds maxStreams.
type Memory struct {
	mu               sync.Mutex
	maxBytesPerStage int
	maxStreams       int
	streams          map[string]*stream
	// lru orders streams by recency of Append; Front is least-recently
	// appended (the eviction victim), Back is most recent. Each
	// stream.elem points back at its node so touch/evict are O(1).
	lru *list.List
}

// stream is one (runID, stageID) stream's retained entries plus its
// running byte total (sum of len(Line) across retained entries) and its
// position in the LRU list.
type stream struct {
	key     string
	entries []Entry
	bytes   int
	elem    *list.Element
}

// NewMemory builds an in-memory store. maxBytesPerStage caps the
// retained Line bytes per stream (oldest entries are dropped first);
// maxStreams caps how many streams are retained (least-recently-appended
// whole stream is evicted first). Non-positive caps are treated as 1 to
// keep the invariants well-defined.
func NewMemory(maxBytesPerStage, maxStreams int) *Memory {
	if maxBytesPerStage < 1 {
		maxBytesPerStage = 1
	}
	if maxStreams < 1 {
		maxStreams = 1
	}
	return &Memory{
		maxBytesPerStage: maxBytesPerStage,
		maxStreams:       maxStreams,
		streams:          make(map[string]*stream),
		lru:              list.New(),
	}
}

// streamKey joins runID and stageID with a NUL so the two segments can
// never collide regardless of their own contents (IDs are UUIDs today,
// but the NUL keeps the key unambiguous if that ever changes).
func streamKey(runID, stageID string) string {
	return runID + "\x00" + stageID
}

// Append records one entry. It appends to the stream's ordered slice,
// evicts oldest entries while the per-stage byte cap is exceeded, marks
// the stream most-recently-used, and evicts whole streams while the
// stream-count cap is exceeded. Always returns nil — the in-memory
// backend cannot fail — but keeps the error return to satisfy Store.
func (m *Memory) Append(_ context.Context, runID, stageID string, e Entry) error {
	key := streamKey(runID, stageID)

	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.streams[key]
	if !ok {
		s = &stream{key: key}
		s.elem = m.lru.PushBack(s)
		m.streams[key] = s
	} else {
		// Most-recently appended → move to the back of the LRU.
		m.lru.MoveToBack(s.elem)
	}

	s.entries = append(s.entries, e)
	s.bytes += len(e.Line)

	// Ring buffer: drop oldest entries until we're back under the cap.
	// Guard against an empty slice so a single line larger than the cap
	// still survives (dropping it would lose the only content).
	for s.bytes > m.maxBytesPerStage && len(s.entries) > 1 {
		s.bytes -= len(s.entries[0].Line)
		s.entries = s.entries[1:]
	}

	// LRU eviction of whole streams once we exceed the stream cap.
	for len(m.streams) > m.maxStreams {
		oldest := m.lru.Front()
		if oldest == nil {
			break
		}
		victim := oldest.Value.(*stream)
		m.lru.Remove(oldest)
		delete(m.streams, victim.key)
	}

	return nil
}

// Read returns a copy of the entries with Seq > since, ascending. The
// returned slice never aliases internal state, so the hub can range over
// it without holding the lock and the executor can keep appending.
func (m *Memory) Read(_ context.Context, runID, stageID string, since int) ([]Entry, error) {
	key := streamKey(runID, stageID)

	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.streams[key]
	if !ok || len(s.entries) == 0 {
		return nil, nil
	}

	// entries are appended in Seq order, so a single linear scan that
	// skips everything <= since yields the ascending tail. Copy each
	// surviving Entry by value into a fresh slice.
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if e.Seq > since {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// Default caps for the memory backend. 1 MiB per stage matches the
// service-side stageLogCap so a stream's replayable history is the same
// order of magnitude as the on-disk capture; 256 concurrent streams
// comfortably covers a single binary's in-flight stages.
const (
	defaultMaxBytesPerStage = 1 << 20
	defaultMaxStreams       = 256
)

// FromEnv selects and constructs a Store from the environment, defaulting
// to the in-memory backend. It never returns nil and never errors: an
// unknown COOKER_LOGSTORE_BACKEND logs a warning and falls back to memory
// so a typo degrades to the safe default rather than failing boot.
//
//	COOKER_LOGSTORE_BACKEND      "memory" (default); anything else warns + falls back
//	COOKER_LOGSTORE_MAX_BYTES    per-stage byte cap (default 1<<20)
//	COOKER_LOGSTORE_MAX_STREAMS  retained stream count (default 256)
func FromEnv() Store {
	backend := os.Getenv("COOKER_LOGSTORE_BACKEND")
	switch backend {
	case "", "memory":
		// fall through to memory construction below
	default:
		slog.Warn("logstore: unknown backend, falling back to memory",
			"backend", backend)
	}
	return NewMemory(
		envInt("COOKER_LOGSTORE_MAX_BYTES", defaultMaxBytesPerStage),
		envInt("COOKER_LOGSTORE_MAX_STREAMS", defaultMaxStreams),
	)
}

// envInt reads a positive integer env var, returning def when unset,
// unparseable, or non-positive.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}
