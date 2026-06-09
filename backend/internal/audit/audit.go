// Package audit provides a structured audit log for mutating API
// requests. Each authenticated POST/PUT/PATCH/DELETE call produces
// one event with the user identity, route template, status, and
// latency — enough for a SIEM to reconstruct who did what when,
// without ever capturing secret-bearing request bodies.
//
// Two sinks ship: stdout (JSON via slog) and file (JSON Lines,
// append-only). Operators select via COOKER_AUDIT_DESTINATION.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/observability"
)

// Event is one audit record. Only fields safe to persist appear here:
// the OIDC subject and email identify the actor; method, path, and
// status describe the action; latency aids capacity analysis. Request
// and response bodies are deliberately absent.
type Event struct {
	Time     time.Time `json:"time"`
	UserSub  string    `json:"user_sub,omitempty"`
	UserMail string    `json:"user_email,omitempty"`
	Method   string    `json:"method"`
	Path     string    `json:"path"`
	Status   int       `json:"status"`
	Latency  string    `json:"latency"`
	ClientIP string    `json:"client_ip,omitempty"`
}

// Sink consumes audit events. Implementations must be safe for
// concurrent use; they are called from request-handling goroutines.
type Sink interface {
	Emit(Event)
	io.Closer
}

// redactedRoutes are paths whose request bodies must never appear in
// the audit trail. The middleware doesn't read bodies in any case,
// but expose the list for callers that want to assert the contract
// (and to document intent at the package boundary). Paths use Gin's
// route-template form (not the raw URL).
var redactedRoutes = map[string]bool{
	"/api/v1/environments/:id/secrets/:key": true,
	"/api/v1/apps/:id/webhook":              true,
}

// IsRedacted reports whether a route's request body must be excluded
// from any audit-related logging. The audit middleware never captures
// bodies, so this is documentation; callers introducing body capture
// in the future MUST consult this function first.
func IsRedacted(routePath string) bool {
	return redactedRoutes[routePath]
}

// NewStdoutSink emits events as JSON via slog. Use slog.NewJSONHandler
// upstream so the entire process log stream is uniform JSON.
func NewStdoutSink(logger *slog.Logger) Sink {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &stdoutSink{logger: logger}
}

type stdoutSink struct {
	logger *slog.Logger
}

func (s *stdoutSink) Emit(e Event) {
	s.logger.Info("audit",
		slog.Time("time", e.Time),
		slog.String("user_sub", e.UserSub),
		slog.String("user_email", e.UserMail),
		slog.String("method", e.Method),
		slog.String("path", e.Path),
		slog.Int("status", e.Status),
		slog.String("latency", e.Latency),
		slog.String("client_ip", e.ClientIP),
	)
}

func (s *stdoutSink) Close() error { return nil }

// fileSinkBuffer is the queue depth for the async writer. Picked so
// a brief disk hiccup (or a sink consumer that's mid-rotation) can
// be absorbed without dropping events under typical load. On
// overflow the sink drops events and bumps a counter the operator
// can probe via observability.IncAuditDropped.
const fileSinkBuffer = 1024

// NewFileSink opens path for append-write of JSON Lines. Emit is
// non-blocking: events are queued onto a bounded channel and a
// single writer goroutine drains the queue. Drop-on-full means a
// disk-full or slow-disk scenario can no longer pin every
// authenticated request behind a held mutex (the failure mode
// flagged in audit T16).
func NewFileSink(path string) (Sink, error) {
	if path == "" {
		return nil, fmt.Errorf("audit: file sink: path is empty")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("audit: file sink: mkdir %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("audit: file sink: open %s: %w", path, err)
	}
	s := &fileSink{
		f:    f,
		ch:   make(chan Event, fileSinkBuffer),
		done: make(chan struct{}),
	}
	go s.run()
	return s, nil
}

type fileSink struct {
	f       *os.File
	ch      chan Event
	done    chan struct{}
	mu      sync.Mutex // guards f.Write inside run() and Close
	closed  bool
	dropped uint64
}

// Emit queues the event for async writing. Non-blocking: under
// backpressure the event is dropped and the dropped counter
// increments. The caller's request thread never waits on disk.
func (s *fileSink) Emit(e Event) {
	select {
	case s.ch <- e:
	default:
		observability.IncAuditDropped()
		s.mu.Lock()
		s.dropped++
		dropped := s.dropped
		s.mu.Unlock()
		// Surface the first drop and then every 1000th to avoid
		// hot-logging when the queue is sustained-full.
		if dropped == 1 || dropped%1000 == 0 {
			slog.Warn("audit: file sink overflow; events dropped", "dropped", dropped)
		}
	}
}

// run drains the queue until Close. It does not hold a mutex during
// the queue receive so producers never block on a slow disk.
func (s *fileSink) run() {
	defer close(s.done)
	for e := range s.ch {
		b, err := json.Marshal(e)
		if err != nil {
			continue
		}
		s.mu.Lock()
		if s.f != nil {
			_, _ = s.f.Write(append(b, '\n'))
		}
		s.mu.Unlock()
	}
}

func (s *fileSink) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	close(s.ch)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Close()
}

// EventWriter is the narrow persistence interface the db sink needs.
// Satisfied by an adapter over store.AuditEventStore (the audit
// package stays store-agnostic).
type EventWriter interface {
	WriteAuditEvent(e Event) error
}

// storeSinkBuffer mirrors fileSinkBuffer: deep enough to absorb a
// burst or a brief Postgres stall; on overflow events drop with a
// counter rather than ever blocking a request goroutine.
const storeSinkBuffer = 1024

// NewStoreSink returns an async Sink that persists events through w
// (roadmap M5: the queryable audit trail). Same drop-on-full contract
// as the file sink: a slow or down database loses audit events, it
// never freezes the API.
func NewStoreSink(w EventWriter) Sink {
	s := &storeSink{
		w:  w,
		ch: make(chan Event, storeSinkBuffer),
	}
	s.done = make(chan struct{})
	go s.run()
	return s
}

type storeSink struct {
	w    EventWriter
	ch   chan Event
	done chan struct{}

	mu      sync.Mutex
	closed  bool
	dropped uint64
}

func (s *storeSink) Emit(e Event) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	select {
	case s.ch <- e:
	default:
		observability.IncAuditDropped()
		s.mu.Lock()
		s.dropped++
		dropped := s.dropped
		s.mu.Unlock()
		if dropped == 1 || dropped%100 == 0 {
			slog.Warn("audit: db sink overflow; dropping events", "dropped_total", dropped)
		}
	}
}

func (s *storeSink) run() {
	defer close(s.done)
	for e := range s.ch {
		if err := s.w.WriteAuditEvent(e); err != nil {
			slog.Warn("audit: db sink write failed", "err", err)
		}
	}
}

// Close drains queued events then stops the writer goroutine.
func (s *storeSink) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	close(s.ch)
	<-s.done
	return nil
}

// NewMultiSink fans Emit out to every sink and Closes them all
// (first error wins). Lets COOKER_AUDIT_DESTINATION carry a comma
// list ("db,stdout") without touching the middleware or the Sink
// interface.
func NewMultiSink(sinks ...Sink) Sink {
	return multiSink(sinks)
}

type multiSink []Sink

func (m multiSink) Emit(e Event) {
	for _, s := range m {
		s.Emit(e)
	}
}

func (m multiSink) Close() error {
	var first error
	for _, s := range m {
		if err := s.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// LatencyMS parses the event's latency string ("12.3ms", "1.2s")
// into milliseconds for storage; unparseable values yield 0.
func (e Event) LatencyMS() int64 {
	d, err := time.ParseDuration(e.Latency)
	if err != nil {
		return 0
	}
	return int64(d / time.Millisecond)
}
