// Package logstore provides a bounded, replayable per-stage log history
// so a WebSocket subscriber that connects mid-run (or reconnects) can
// receive the backlog-so-far before the live stream, and a dropped slow
// client can be told the stream was truncated rather than going silent.
//
// Part A, Phases 1-2 of docs/proposals/execution-observability-redesign-2026.md.
// Only the in-memory backend ships here: postgres/redis are future,
// multi-replica concerns and are explicitly out of scope. The Store
// interface mirrors the existing strategy/backend split so a durable
// backend can be slotted in later without touching the service or server.
package logstore

import (
	"context"
	"encoding/json"
	"time"
)

// Entry is a single stamped log line. Seq is a monotonic per-stage
// counter starting at 1; it is the cursor the WS replay path resumes
// from via ?since=<seq>. Line carries NO trailing newline (the wire
// envelope and the on-disk StageRun.Logs capture differ here on
// purpose — the envelope is structured, one frame per line).
type Entry struct {
	Seq  int       `json:"seq"`
	TS   time.Time `json:"ts"`
	Line string    `json:"line"`
}

// Store is the per-stage log history. Append is called from the
// executor's per-stage writer goroutine; Read is called from the WS hub
// goroutine on connect. Implementations MUST be safe for concurrent
// Append and Read.
type Store interface {
	// Append records one entry for (runID, stageID). It is best-effort
	// from the caller's perspective — a returned error never fails the
	// run, but is surfaced so a durable backend can be observed.
	Append(ctx context.Context, runID, stageID string, e Entry) error
	// Read returns a copy of the entries for (runID, stageID) whose Seq
	// is strictly greater than since, in ascending Seq order. since=0
	// returns everything retained. Implementations MUST NOT return a
	// slice that aliases internal state.
	Read(ctx context.Context, runID, stageID string, since int) ([]Entry, error)
}

// wireFrame is the canonical on-the-wire shape of a streamed log line.
// It is pinned: the frontend log viewer decodes exactly these fields and
// dedupes by seq, so the service broadcaster and the server replay path
// MUST produce identical bytes. Do not reorder or rename fields.
type wireFrame struct {
	RunID   string `json:"runId"`
	StageID string `json:"stageId"`
	Seq     int    `json:"seq"`
	TS      string `json:"ts"`
	Line    string `json:"line"`
}

// EncodeFrame builds the canonical JSON wire frame for a log entry so the
// service broadcaster (live frames) and the server replay path (backlog)
// agree byte-for-byte. The timestamp is rendered as RFC3339 with
// millisecond precision in UTC to match the pinned envelope example.
//
// Marshalling four strings and an int never fails; on the impossible
// error path EncodeFrame returns nil and the caller simply skips that
// frame (mirrors service.encodeStatusUpdate).
func EncodeFrame(runID, stageID string, e Entry) []byte {
	b, err := json.Marshal(wireFrame{
		RunID:   runID,
		StageID: stageID,
		Seq:     e.Seq,
		TS:      e.TS.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		Line:    e.Line,
	})
	if err != nil {
		return nil
	}
	return b
}
