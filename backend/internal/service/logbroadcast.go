package service

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/santapong/cooker/internal/logstore"
)

// RunStatusChannel returns the canonical WebSocket channel name on
// which per-stage status updates are published for a run. Matches the
// frontend usePipelineExecution hook's subscription
// (/ws/pipeline-run/:runId → "pipeline-run:<runID>").
func RunStatusChannel(runID string) string {
	return "pipeline-run:" + runID
}

// statusUpdate is the wire shape the canvas consumes to tint nodes.
type statusUpdate struct {
	NodeID string `json:"nodeId"`
	Status string `json:"status"`
}

// encodeStatusUpdate marshals a {nodeId,status} frame. Marshal of two
// strings never fails; on the impossible error path it returns nil and
// the caller simply skips the broadcast.
func encodeStatusUpdate(stageID, status string) []byte {
	b, err := json.Marshal(statusUpdate{NodeID: stageID, Status: status})
	if err != nil {
		return nil
	}
	return b
}

// LogBroadcaster pushes a single log line to a named WebSocket
// channel. Implementations are expected to be non-blocking — the
// concrete adapter in package server uses Hub.Broadcast which drops
// on backpressure rather than blocking the executor goroutine.
//
// The channel format is `stage-logs:<runId>:<stageId>` (see
// StageLogChannel). Subscribers from the frontend connect via
// /ws/runs/:runId/stages/:stageId/logs which translates to the same
// channel string.
type LogBroadcaster func(channel string, line []byte)

// StageLogChannel returns the canonical WebSocket channel name for
// streaming log lines from a single stage's run. The same string is
// used by the executor's broadcaster and by server.WebSocketHub's
// HandleStageLogs handler — they must agree byte-for-byte.
func StageLogChannel(runID, stageID string) string {
	return "stage-logs:" + runID + ":" + stageID
}

// lineWriter splits incoming bytes on '\n' and, for each complete line,
// stamps a monotonic per-stage seq + UTC timestamp, appends the line to
// the log store (for replay-on-connect), and broadcasts the canonical
// JSON wire envelope on the per-stage channel. Partial lines (no trailing
// newline yet) are buffered until the next Write; a stage that finishes
// mid-line gets its leftover partial emitted by flush() so the tail isn't
// lost.
//
// Wire format (pinned — the frontend dedupes by seq, see logstore):
//
//	{"runId":...,"stageId":...,"seq":N,"ts":...,"line":"..."}  // no trailing \n in line
//
// Not goroutine-safe, and it does not need to be: the executor serialises
// Write calls per stage through the builder/pusher/deployer LogWriter
// (one stage == one writer == one goroutine). That single-writer
// invariant is what makes the unsynchronised seq++ correct — there is no
// concurrent writer to race with on this struct.
type lineWriter struct {
	broadcast LogBroadcaster
	store     logstore.Store
	runID     string
	stageID   string
	channel   string
	seq       int
	partial   []byte
}

// newLineWriter builds a lineWriter for one stage. Either broadcast or
// store (or both) may be set; the channel is derived internally so the
// service and the server replay path agree on StageLogChannel. When both
// are nil the writer is an inert passthrough (Write still returns
// len(p)).
func newLineWriter(broadcast LogBroadcaster, store logstore.Store, runID, stageID string) *lineWriter {
	return &lineWriter{
		broadcast: broadcast,
		store:     store,
		runID:     runID,
		stageID:   stageID,
		channel:   StageLogChannel(runID, stageID),
	}
}

// emit records a single complete line (without its trailing newline):
// it bumps seq, builds the Entry, appends to the store (best-effort), and
// broadcasts the encoded envelope. line must already be free of any '\n'.
func (w *lineWriter) emit(line string) {
	w.seq++
	e := logstore.Entry{Seq: w.seq, TS: time.Now().UTC(), Line: line}
	if w.store != nil {
		// Best-effort: a store append failure must never fail the run.
		// The memory backend cannot error; a future durable backend's
		// error is swallowed here (the canonical post-run log remains
		// StageRun.Logs).
		_ = w.store.Append(context.Background(), w.runID, w.stageID, e)
	}
	if w.broadcast != nil {
		if frame := logstore.EncodeFrame(w.runID, w.stageID, e); frame != nil {
			w.broadcast(w.channel, frame)
		}
	}
}

// Write splits p on newlines and emits each complete line. The returned
// int is always len(p): the store/broadcast legs are best-effort, so a
// Hub backpressure drop or store hiccup must not propagate as a write
// error to the builder's stdio pipe. This "always returns len(p)"
// invariant is load-bearing: executeBuild wraps lineWriter in
// io.MultiWriter, which aborts the entire write chain on a short-count
// return (W10-1).
func (w *lineWriter) Write(p []byte) (int, error) {
	if w == nil || (w.broadcast == nil && w.store == nil) {
		return len(p), nil
	}
	if len(p) == 0 {
		return 0, nil
	}
	combined := p
	if len(w.partial) > 0 {
		// Slow path: prepend the carried-over partial. Allocates only
		// when a newline straddled a Write boundary.
		buf := make([]byte, 0, len(w.partial)+len(p))
		buf = append(buf, w.partial...)
		buf = append(buf, p...)
		combined = buf
		w.partial = w.partial[:0]
	}
	for {
		i := bytes.IndexByte(combined, '\n')
		if i < 0 {
			// No newline left — buffer the tail and return.
			if len(combined) > 0 {
				w.partial = append(w.partial[:0], combined...)
			}
			break
		}
		// Strip the trailing '\n': the envelope's Line carries no
		// newline (the frontend re-adds the separator on render).
		w.emit(string(combined[:i]))
		combined = combined[i+1:]
	}
	return len(p), nil
}

// flush emits any buffered partial line as a final envelope so a stage
// that ends without a trailing newline (e.g. a failed build whose last
// write didn't include one) doesn't lose its tail. Called from the
// deferred cleanup in executeBuild/executePush/executeDeploy.
func (w *lineWriter) flush() {
	if w == nil || (w.broadcast == nil && w.store == nil) || len(w.partial) == 0 {
		return
	}
	w.emit(string(w.partial))
	w.partial = w.partial[:0]
}
