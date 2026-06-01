package service

import (
	"bytes"
	"encoding/json"
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

// lineWriter splits incoming bytes on '\n' and invokes broadcast once
// per complete line. Partial lines (no trailing newline yet) are
// buffered until the next Write; on the rare path where a stage
// finishes mid-line the leftover partial is dropped — the canonical
// log is the StageRun.Logs append on disk, not the live stream.
//
// Not goroutine-safe. The executor serialises Write calls per stage via
// the builder's LogWriter; do not share a lineWriter across goroutines.
type lineWriter struct {
	broadcast LogBroadcaster
	channel   string
	partial   []byte
}

// newLineWriter builds a lineWriter that emits to the given channel.
// broadcast must be non-nil; callers should use io.Discard semantics
// (skip the wrapper entirely) when the executor has no broadcaster
// configured.
func newLineWriter(broadcast LogBroadcaster, channel string) *lineWriter {
	return &lineWriter{broadcast: broadcast, channel: channel}
}

// Write splits p on newlines and broadcasts each complete line. The
// returned int is always len(p): the broadcast leg is best-effort, so
// a Hub backpressure drop must not propagate as a write error to the
// builder's stdio pipe. This "always returns len(p)" invariant is
// load-bearing: executeBuild wraps lineWriter in io.MultiWriter, which
// aborts the entire write chain on a short-count return (W10-1).
func (w *lineWriter) Write(p []byte) (int, error) {
	if w == nil || w.broadcast == nil {
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
		// Emit one line including the trailing newline; the frontend
		// log viewer expects "\n" as the line separator on the wire so
		// the rendered terminal matches what was captured on disk.
		line := combined[:i+1]
		// Copy out of `combined` because the caller may reuse p.
		out := make([]byte, len(line))
		copy(out, line)
		w.broadcast(w.channel, out)
		combined = combined[i+1:]
	}
	return len(p), nil
}

// flush emits any buffered partial line as a final broadcast so a
// stage that ends without a trailing newline (e.g. a failed build
// whose last write didn't include one) doesn't lose its tail. Called
// from the deferred cleanup in executeBuild.
func (w *lineWriter) flush() {
	if w == nil || w.broadcast == nil || len(w.partial) == 0 {
		return
	}
	out := make([]byte, len(w.partial)+1)
	copy(out, w.partial)
	out[len(w.partial)] = '\n'
	w.broadcast(w.channel, out)
	w.partial = w.partial[:0]
}
