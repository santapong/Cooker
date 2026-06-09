package audit

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsRedacted(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/api/v1/environments/:id/secrets/:key", true},
		{"/api/v1/apps/:id/webhook", true},
		{"/api/v1/pipelines/:id/run", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsRedacted(tc.path); got != tc.want {
			t.Errorf("IsRedacted(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestStdoutSink_Emit(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	sink := NewStdoutSink(logger)

	sink.Emit(Event{
		Time:     time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
		UserSub:  "user-123",
		UserMail: "alice@example.com",
		Method:   "POST",
		Path:     "/api/v1/pipelines/:id/run",
		Status:   200,
		Latency:  "42ms",
		ClientIP: "10.0.0.1",
	})
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if out["msg"] != "audit" {
		t.Errorf("msg = %v, want audit", out["msg"])
	}
	if out["user_sub"] != "user-123" {
		t.Errorf("user_sub = %v, want user-123", out["user_sub"])
	}
	if out["user_email"] != "alice@example.com" {
		t.Errorf("user_email = %v, want alice@example.com", out["user_email"])
	}
	if out["method"] != "POST" {
		t.Errorf("method = %v, want POST", out["method"])
	}
	// status decodes as float64 from generic JSON
	if v, ok := out["status"].(float64); !ok || int(v) != 200 {
		t.Errorf("status = %v, want 200", out["status"])
	}
	// Negative assertion: no body, no token, no secret-y key.
	for _, forbidden := range []string{"body", "token", "secret", "value", "password"} {
		if _, present := out[forbidden]; present {
			t.Errorf("event must not contain key %q; got %v", forbidden, out)
		}
	}
}

func TestFileSink_AppendsJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "audit.log")
	sink, err := NewFileSink(path)
	if err != nil {
		t.Fatalf("NewFileSink: %v", err)
	}

	events := []Event{
		{Time: time.Unix(1, 0).UTC(), Method: "POST", Path: "/a", Status: 201},
		{Time: time.Unix(2, 0).UTC(), Method: "DELETE", Path: "/b", Status: 204},
	}
	for _, e := range events {
		sink.Emit(e)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readfile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(contents))
	}
	for i, line := range lines {
		var got Event
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d: unmarshal %q: %v", i, line, err)
		}
		if got.Method != events[i].Method || got.Status != events[i].Status {
			t.Errorf("line %d: got %+v, want %+v", i, got, events[i])
		}
	}
}

func TestFileSink_ConcurrentEmits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	sink, err := NewFileSink(path)
	if err != nil {
		t.Fatalf("NewFileSink: %v", err)
	}
	defer sink.Close()

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			sink.Emit(Event{Method: "POST", Path: "/x", Status: 200})
		}()
	}
	wg.Wait()
	_ = sink.Close()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readfile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
	if len(lines) != N {
		t.Fatalf("expected %d lines, got %d", N, len(lines))
	}
	for i, line := range lines {
		var got Event
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Errorf("line %d: invalid JSON %q: %v (concurrent writes interleaved)", i, line, err)
		}
	}
}

func TestNewFileSink_EmptyPath(t *testing.T) {
	if _, err := NewFileSink(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

// blockingWriter lets tests hold the storeSink's writer goroutine
// mid-write so the channel can be filled to overflow deterministically.
type blockingWriter struct {
	mu      sync.Mutex
	events  []Event
	release chan struct{} // non-nil: each write waits for one receive
	fail    error
}

func (w *blockingWriter) WriteAuditEvent(e Event) error {
	if w.release != nil {
		<-w.release
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail != nil {
		return w.fail
	}
	w.events = append(w.events, e)
	return nil
}

func (w *blockingWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.events)
}

func TestStoreSink_CloseDrainsQueue(t *testing.T) {
	w := &blockingWriter{}
	s := NewStoreSink(w)
	const N = 200
	for i := 0; i < N; i++ {
		s.Emit(Event{Method: "POST", Path: "/x", Status: 201, Latency: "12ms"})
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := w.count(); got != N {
		t.Fatalf("Close did not drain: wrote %d of %d", got, N)
	}
}

func TestStoreSink_DropOnFullNeverBlocks(t *testing.T) {
	w := &blockingWriter{release: make(chan struct{})}
	s := NewStoreSink(w)

	// One event is pulled into the (blocked) writer; fill the channel
	// behind it, then keep emitting — every extra must return
	// immediately and be dropped, not block the producer.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < storeSinkBuffer+50; i++ {
			s.Emit(Event{Method: "POST", Path: "/x"})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Emit blocked on a full queue")
	}

	// A closed channel unblocks every pending and future receive, so
	// the writer drains freely from here.
	close(w.release)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := w.count(); got > storeSinkBuffer+1 {
		t.Fatalf("wrote %d events, want <= %d (rest dropped)", got, storeSinkBuffer+1)
	}
}

func TestStoreSink_EmitAfterCloseIsNoop(t *testing.T) {
	w := &blockingWriter{}
	s := NewStoreSink(w)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s.Emit(Event{Method: "POST", Path: "/x"}) // must not panic on closed channel
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// recordSink is a synchronous in-memory Sink for MultiSink tests.
type recordSink struct {
	mu     sync.Mutex
	events []Event
	closed bool
	err    error
}

func (r *recordSink) Emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordSink) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return r.err
}

func TestMultiSink_FansOutAndCloses(t *testing.T) {
	a, b := &recordSink{}, &recordSink{err: os.ErrClosed}
	m := NewMultiSink(a, b)
	m.Emit(Event{Method: "DELETE", Path: "/y", Status: 204})
	if len(a.events) != 1 || len(b.events) != 1 {
		t.Fatalf("fan-out missed a sink: a=%d b=%d", len(a.events), len(b.events))
	}
	if err := m.Close(); err != os.ErrClosed {
		t.Fatalf("Close should surface the first sink error, got %v", err)
	}
	if !a.closed || !b.closed {
		t.Fatal("Close must reach every sink even after an error")
	}
}

func TestEventLatencyMS(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"12ms", 12},
		{"1.5s", 1500},
		{"250µs", 0},
		{"not-a-duration", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := (Event{Latency: tc.in}).LatencyMS(); got != tc.want {
			t.Errorf("LatencyMS(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
