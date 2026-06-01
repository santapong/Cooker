package server

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"github.com/santapong/cooker/internal/logstore"
)

// stageLogChannel is the canonical name for a per-stage log stream.
// Must agree byte-for-byte with service.StageLogChannel — duplicated
// here to avoid an import cycle (service depends on no server type;
// server formats the same string for the hub-side handler).
func stageLogChannel(runID, stageID string) string {
	return "stage-logs:" + runID + ":" + stageID
}

// HandleStageLogs upgrades an HTTP request to a WebSocket, replays the
// per-stage log backlog from the hub's log store, then subscribes the
// client to live frames on the per-stage channel. Each frame (backlog
// and live) is the canonical JSON envelope logstore.EncodeFrame produces:
//
//	{"runId":...,"stageId":...,"seq":N,"ts":...,"line":"..."}
//
// Replay-on-connect (execution-observability redesign Part A Phase 2):
//
//	?since=<seq>  resume after seq N (default 0 → from the start of what
//	              the bounded store still retains). Negative values clamp
//	              to 0.
//
// Ordering / single-writer invariant: we register the client BEFORE
// replaying so no live frame is lost between "end of backlog" and
// "subscribed" — live frames buffer in client.send while we write the
// backlog. The backlog is written DIRECTLY to conn here, before
// writePump starts, so exactly one goroutine ever writes conn (this
// function, then writePump — never both at once). The frontend dedupes
// by seq, so a live frame that also appears in the backlog is harmless.
//
// The caller (router.go) gates this with the standard 60s ws ticket flow
// before invoking this method, so the request is already authenticated.
func (h *WebSocketHub) HandleStageLogs(w http.ResponseWriter, r *http.Request, runID, stageID string) {
	// 1. Parse the resume cursor. Anything unparseable or negative means
	//    "from the start" (since=0) rather than an error — a stale/garbage
	//    cursor should degrade to a full replay, not a failed connect.
	since := 0
	if raw := r.URL.Query().Get("since"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			since = n
		}
	}

	// 2. Upgrade.
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}

	// 3. Build the client and 4. register it so live frames start
	//    buffering in client.send immediately (closing the gap with the
	//    backlog replay below).
	client := &Client{
		hub:     h,
		conn:    conn,
		send:    make(chan []byte, 256),
		channel: stageLogChannel(runID, stageID),
	}
	h.register <- client

	// 5. Replay the backlog directly on conn (single conn-writer: writePump
	//    has not started yet). On any write error tear the client down and
	//    bail before starting the pumps.
	if h.logStore != nil {
		entries, _ := h.logStore.Read(r.Context(), runID, stageID, since)
		for _, e := range entries {
			frame := logstore.EncodeFrame(runID, stageID, e)
			if frame == nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if werr := conn.WriteMessage(websocket.TextMessage, frame); werr != nil {
				// Client went away mid-replay: unregister (so the hub stops
				// buffering) and close. Do NOT start the pumps.
				h.unregister <- client
				_ = conn.Close()
				return
			}
		}
	}

	// 6. Hand conn over to the pumps. From here writePump is the sole
	//    conn writer.
	go client.writePump()
	go client.readPump()
}
