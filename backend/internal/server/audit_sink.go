package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/audit"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// auditDestinations splits the COOKER_AUDIT_DESTINATION comma list
// into trimmed entries; empty input means the stdout default.
func auditDestinations(dest string) []string {
	var out []string
	for _, p := range strings.Split(dest, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []string{"stdout"}
	}
	return out
}

func auditHasDB(dest string) bool {
	for _, d := range auditDestinations(dest) {
		if d == "db" {
			return true
		}
	}
	return false
}

// newAuditSink builds the sink stack for COOKER_AUDIT_DESTINATION.
// The value is a comma list (e.g. "db,stdout"); a single entry
// returns that sink bare, several fan out via MultiSink. The store
// backs the "db" sink — the audit middleware itself is untouched.
func newAuditSink(cfg config.AuditConfig, st *store.Store) (audit.Sink, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	var sinks []audit.Sink
	closeAll := func() {
		for _, s := range sinks {
			_ = s.Close()
		}
	}
	seen := map[string]bool{}
	for _, dest := range auditDestinations(cfg.Destination) {
		if seen[dest] {
			continue
		}
		seen[dest] = true
		switch dest {
		case "stdout":
			sinks = append(sinks, audit.NewStdoutSink(nil))
		case "file":
			fs, err := audit.NewFileSink(cfg.FilePath)
			if err != nil {
				closeAll()
				return nil, err
			}
			sinks = append(sinks, fs)
		case "db":
			if st == nil || st.AuditEvents == nil {
				closeAll()
				return nil, fmt.Errorf("destination \"db\" requires a store with audit support")
			}
			sinks = append(sinks, audit.NewStoreSink(auditStoreWriter{events: st.AuditEvents}))
		default:
			closeAll()
			return nil, fmt.Errorf("unknown destination %q", dest)
		}
	}
	if len(sinks) == 1 {
		return sinks[0], nil
	}
	return audit.NewMultiSink(sinks...), nil
}

// auditStoreWriter adapts store.AuditEventStore to audit.EventWriter.
// Runs on the storeSink's single writer goroutine, never a request
// path, so a generous per-insert timeout is safe.
type auditStoreWriter struct {
	events store.AuditEventStore
}

func (w auditStoreWriter) WriteAuditEvent(e audit.Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.events.Insert(ctx, &model.AuditEvent{
		Time:      e.Time,
		UserSub:   e.UserSub,
		UserEmail: e.UserMail,
		Method:    e.Method,
		Path:      e.Path,
		Status:    e.Status,
		LatencyMS: e.LatencyMS(),
		ClientIP:  e.ClientIP,
	})
}
