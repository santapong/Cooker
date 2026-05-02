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

// NewFileSink opens path for append-write of JSON Lines. Each call to
// Emit writes one JSON object followed by '\n'. Concurrent Emits are
// serialised via a mutex; a buffered channel would lose events on
// crash, so we accept the lock cost.
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
	return &fileSink{f: f}, nil
}

type fileSink struct {
	mu sync.Mutex
	f  *os.File
}

func (s *fileSink) Emit(e Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.f.Write(append(b, '\n'))
}

func (s *fileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Close()
}
