package server

import (
	"net/http"
)

// stageLogChannel is the canonical name for a per-stage log stream.
// Must agree byte-for-byte with service.StageLogChannel — duplicated
// here to avoid an import cycle (service depends on no server type;
// server formats the same string for the hub-side handler).
func stageLogChannel(runID, stageID string) string {
	return "stage-logs:" + runID + ":" + stageID
}

// HandleStageLogs upgrades an HTTP request to a WebSocket and
// subscribes the client to the per-stage log channel for the given
// (runID, stageID) pair. Lines emitted by the executor's
// LogBroadcaster arrive as TextMessages in the order they were
// written.
//
// The caller (router.go) gates this with the standard 60s ws ticket
// flow before invoking this method, so by the time we reach here
// the request is already authenticated.
func (h *WebSocketHub) HandleStageLogs(w http.ResponseWriter, r *http.Request, runID, stageID string) {
	h.handleConnection(w, r, stageLogChannel(runID, stageID))
}
