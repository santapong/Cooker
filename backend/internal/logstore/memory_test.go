package logstore

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func entry(seq int, line string) Entry {
	return Entry{Seq: seq, TS: time.Unix(0, 0).UTC(), Line: line}
}

func appendAll(t *testing.T, s Store, runID, stageID string, es ...Entry) {
	t.Helper()
	for _, e := range es {
		if err := s.Append(context.Background(), runID, stageID, e); err != nil {
			t.Fatalf("Append seq=%d: %v", e.Seq, err)
		}
	}
}

func readSeqs(t *testing.T, s Store, runID, stageID string, since int) []int {
	t.Helper()
	got, err := s.Read(context.Background(), runID, stageID, since)
	if err != nil {
		t.Fatalf("Read(since=%d): %v", since, err)
	}
	seqs := make([]int, len(got))
	for i, e := range got {
		seqs[i] = e.Seq
	}
	return seqs
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMemory_ReadFromZeroReturnsAllAscending(t *testing.T) {
	m := NewMemory(1<<20, 16)
	appendAll(t, m, "r", "s", entry(1, "a"), entry(2, "b"), entry(3, "c"))

	if got := readSeqs(t, m, "r", "s", 0); !equalInts(got, []int{1, 2, 3}) {
		t.Fatalf("Read(0) seqs = %v, want [1 2 3]", got)
	}
}

func TestMemory_ReadSinceFiltersBySeq(t *testing.T) {
	m := NewMemory(1<<20, 16)
	appendAll(t, m, "r", "s", entry(1, "a"), entry(2, "b"), entry(3, "c"))

	if got := readSeqs(t, m, "r", "s", 2); !equalInts(got, []int{3}) {
		t.Fatalf("Read(2) seqs = %v, want [3]", got)
	}
	if got := readSeqs(t, m, "r", "s", 3); len(got) != 0 {
		t.Fatalf("Read(3) seqs = %v, want []", got)
	}
}

func TestMemory_UnknownStreamReadsEmpty(t *testing.T) {
	m := NewMemory(1<<20, 16)
	if got := readSeqs(t, m, "nope", "nope", 0); len(got) != 0 {
		t.Fatalf("Read of unknown stream = %v, want []", got)
	}
}

func TestMemory_PerStageByteCapEvictsOldest(t *testing.T) {
	// Cap small so a handful of 4-byte lines overflows. Each "aaaa" is 4
	// bytes; cap at 10 retains at most the newest entries summing <= 10.
	m := NewMemory(10, 16)
	appendAll(t, m, "r", "s",
		entry(1, "aaaa"), // 4
		entry(2, "bbbb"), // 8
		entry(3, "cccc"), // 12 -> over cap, drop seq 1 -> 8
		entry(4, "dddd"), // 12 -> over cap, drop seq 2 -> 8
	)
	// Retained should be the newest fitting the cap: seq 3 and 4 (8 bytes).
	if got := readSeqs(t, m, "r", "s", 0); !equalInts(got, []int{3, 4}) {
		t.Fatalf("after byte-cap eviction seqs = %v, want [3 4]", got)
	}
}

func TestMemory_OversizeSingleLineSurvives(t *testing.T) {
	// A single line larger than the whole cap must still be retained —
	// dropping it would leave the stream empty and lose all content.
	m := NewMemory(4, 16)
	appendAll(t, m, "r", "s", entry(1, strings.Repeat("x", 100)))
	if got := readSeqs(t, m, "r", "s", 0); !equalInts(got, []int{1}) {
		t.Fatalf("oversize single line seqs = %v, want [1]", got)
	}
}

func TestMemory_MaxStreamsEvictsOldestStream(t *testing.T) {
	m := NewMemory(1<<20, 2)
	// Three distinct streams, appended in order s1, s2, s3. With cap=2 the
	// least-recently-appended stream (s1) is evicted on the third stream's
	// first append.
	appendAll(t, m, "r", "s1", entry(1, "one"))
	appendAll(t, m, "r", "s2", entry(1, "two"))
	appendAll(t, m, "r", "s3", entry(1, "three"))

	if got := readSeqs(t, m, "r", "s1", 0); len(got) != 0 {
		t.Fatalf("s1 should be evicted, got %v", got)
	}
	if got := readSeqs(t, m, "r", "s2", 0); !equalInts(got, []int{1}) {
		t.Fatalf("s2 should be retained, got %v", got)
	}
	if got := readSeqs(t, m, "r", "s3", 0); !equalInts(got, []int{1}) {
		t.Fatalf("s3 should be retained, got %v", got)
	}
}

func TestMemory_AppendTouchesLRURecency(t *testing.T) {
	m := NewMemory(1<<20, 2)
	appendAll(t, m, "r", "s1", entry(1, "one"))
	appendAll(t, m, "r", "s2", entry(1, "two"))
	// Touch s1 so it becomes most-recent; s2 is now the eviction victim.
	appendAll(t, m, "r", "s1", entry(2, "one-again"))
	appendAll(t, m, "r", "s3", entry(1, "three"))

	if got := readSeqs(t, m, "r", "s1", 0); !equalInts(got, []int{1, 2}) {
		t.Fatalf("s1 was touched, should be retained, got %v", got)
	}
	if got := readSeqs(t, m, "r", "s2", 0); len(got) != 0 {
		t.Fatalf("s2 was least-recent, should be evicted, got %v", got)
	}
}

func TestMemory_ReadReturnsCopyNotInternalSlice(t *testing.T) {
	m := NewMemory(1<<20, 16)
	appendAll(t, m, "r", "s", entry(1, "a"))
	got, err := m.Read(context.Background(), "r", "s", 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Mutate the returned slice; a subsequent Read must be unaffected.
	got[0].Line = "MUTATED"
	again, err := m.Read(context.Background(), "r", "s", 0)
	if err != nil {
		t.Fatalf("Read again: %v", err)
	}
	if again[0].Line != "a" {
		t.Fatalf("internal state mutated via returned slice: got %q", again[0].Line)
	}
}

// TestMemory_ConcurrentAppendRead drives Append (executor goroutine) and
// Read (hub goroutine) on the same stream simultaneously. Under -race a
// missing lock would be flagged; we also assert no panic and a final
// consistent read.
func TestMemory_ConcurrentAppendRead(t *testing.T) {
	m := NewMemory(1<<20, 16)
	const n = 2000

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 1; i <= n; i++ {
			_ = m.Append(context.Background(), "r", "s", entry(i, "line"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			es, _ := m.Read(context.Background(), "r", "s", 0)
			// Touch the result so the race detector sees the read of the
			// returned slice; also assert ascending ordering holds.
			prev := 0
			for _, e := range es {
				if e.Seq <= prev {
					t.Errorf("non-ascending seq in concurrent read: %d after %d", e.Seq, prev)
					return
				}
				prev = e.Seq
			}
		}
	}()

	wg.Wait()

	if got := readSeqs(t, m, "r", "s", n-1); !equalInts(got, []int{n}) {
		t.Fatalf("final Read(%d) = %v, want [%d]", n-1, got, n)
	}
}

func TestEncodeFrame_PinnedShape(t *testing.T) {
	ts := time.Date(2026, 5, 30, 15, 34, 18, 221_000_000, time.UTC)
	got := EncodeFrame("run-1", "stage-x", Entry{Seq: 1421, TS: ts, Line: "Step 3/7 ..."})

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("EncodeFrame produced invalid JSON: %v (%s)", err, got)
	}
	want := map[string]any{
		"runId":   "run-1",
		"stageId": "stage-x",
		"seq":     float64(1421),
		"ts":      "2026-05-30T15:34:18.221Z",
		"line":    "Step 3/7 ...",
	}
	for k, v := range want {
		if decoded[k] != v {
			t.Errorf("frame[%q] = %v (%T), want %v (%T)", k, decoded[k], decoded[k], v, v)
		}
	}
	if len(decoded) != len(want) {
		t.Errorf("frame has %d keys, want %d: %s", len(decoded), len(want), got)
	}
}

func TestEncodeFrame_NoTrailingNewlineInLine(t *testing.T) {
	got := EncodeFrame("r", "s", Entry{Seq: 1, TS: time.Unix(0, 0).UTC(), Line: "no newline"})
	var decoded wireFrame
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if strings.HasSuffix(decoded.Line, "\n") {
		t.Fatalf("line carried a trailing newline: %q", decoded.Line)
	}
}
