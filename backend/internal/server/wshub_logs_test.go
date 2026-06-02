package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/santapong/cooker/internal/logstore"
)

// --- test helpers -----------------------------------------------------

// newLogHub spins up a hub with the in-memory backend, starts its Run
// loop, and registers cleanup. allowedOrigins is "*" so the httptest
// dialer's Origin is accepted.
func newLogHub(t *testing.T) *WebSocketHub {
	t.Helper()
	h := NewWebSocketHub([]string{"*"})
	go h.Run()
	t.Cleanup(func() { _ = h.Close() })
	return h
}

// stageLogServer wires HandleStageLogs behind an httptest server for the
// fixed (runID, stageID). The returned ws:// URL accepts a ?since= query.
func stageLogServer(t *testing.T, h *WebSocketHub, runID, stageID string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleStageLogs(w, r, runID, stageID)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// dial opens a WS connection to url (which may carry ?since=). Fails the
// test on a handshake error.
func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// readFrame reads one TextMessage and decodes its {seq,line,control}
// fields with a deadline so a hang fails the test instead of blocking.
type wsFrame struct {
	Seq     int    `json:"seq"`
	Line    string `json:"line"`
	Control string `json:"control"`
}

func readFrame(t *testing.T, conn *websocket.Conn) wsFrame {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if mt != websocket.TextMessage {
		t.Fatalf("frame message type = %d, want TextMessage", mt)
	}
	var f wsFrame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("decode frame %q: %v", data, err)
	}
	return f
}

// waitRegistered blocks until at least one client is registered on
// channel, so a subsequent Broadcast is guaranteed to reach it (closing
// the register/inbox ordering gap in Run's select).
func waitRegistered(t *testing.T, h *WebSocketHub, channel string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		n := len(h.clients[channel])
		h.mu.RUnlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no client registered on %q within deadline", channel)
}

func appendN(t *testing.T, s logstore.Store, runID, stageID string, lines ...string) {
	t.Helper()
	for i, line := range lines {
		e := logstore.Entry{Seq: i + 1, TS: time.Unix(0, 0).UTC(), Line: line}
		if err := s.Append(context.Background(), runID, stageID, e); err != nil {
			t.Fatalf("append seq=%d: %v", i+1, err)
		}
	}
}

// --- tests ------------------------------------------------------------

// TestHandleStageLogs_MidRunJoinReplaysBacklog: a client joining with
// since=0 receives the full backlog (seq 1..3) in order, then a live 4th
// frame appended+broadcast after connect.
func TestHandleStageLogs_MidRunJoinReplaysBacklog(t *testing.T) {
	h := newLogHub(t)
	store := logstore.NewMemory(1<<20, 16)
	h.SetLogStore(store)

	const runID, stageID = "run-1", "stage-1"
	appendN(t, store, runID, stageID, "line-1", "line-2", "line-3")

	channel := stageLogChannel(runID, stageID)
	conn := dial(t, stageLogServer(t, h, runID, stageID)+"?since=0")

	for i := 1; i <= 3; i++ {
		f := readFrame(t, conn)
		if f.Seq != i {
			t.Fatalf("backlog frame %d: seq = %d, want %d", i, f.Seq, i)
		}
	}

	// Live frame: append + broadcast the canonical envelope after the
	// client is registered. The frontend dedupes by seq, but seq 4 is new.
	waitRegistered(t, h, channel)
	live := logstore.Entry{Seq: 4, TS: time.Unix(0, 0).UTC(), Line: "line-4"}
	if err := store.Append(context.Background(), runID, stageID, live); err != nil {
		t.Fatalf("append live: %v", err)
	}
	h.Broadcast(channel, logstore.EncodeFrame(runID, stageID, live))

	if f := readFrame(t, conn); f.Seq != 4 || f.Line != "line-4" {
		t.Fatalf("live frame: got seq=%d line=%q, want seq=4 line=line-4", f.Seq, f.Line)
	}
}

// TestHandleStageLogs_ReconnectSinceFiltersBacklog: a client reconnecting
// with since=2 receives only seq 3 from the backlog, then a live frame.
func TestHandleStageLogs_ReconnectSinceFiltersBacklog(t *testing.T) {
	h := newLogHub(t)
	store := logstore.NewMemory(1<<20, 16)
	h.SetLogStore(store)

	const runID, stageID = "run-2", "stage-2"
	appendN(t, store, runID, stageID, "line-1", "line-2", "line-3")

	channel := stageLogChannel(runID, stageID)
	conn := dial(t, stageLogServer(t, h, runID, stageID)+"?since=2")

	// Only seq 3 should replay.
	if f := readFrame(t, conn); f.Seq != 3 || f.Line != "line-3" {
		t.Fatalf("first replayed frame: got seq=%d line=%q, want seq=3 line=line-3", f.Seq, f.Line)
	}

	// Then a live seq 4.
	waitRegistered(t, h, channel)
	live := logstore.Entry{Seq: 4, TS: time.Unix(0, 0).UTC(), Line: "line-4"}
	if err := store.Append(context.Background(), runID, stageID, live); err != nil {
		t.Fatalf("append live: %v", err)
	}
	h.Broadcast(channel, logstore.EncodeFrame(runID, stageID, live))

	if f := readFrame(t, conn); f.Seq != 4 {
		t.Fatalf("live frame after reconnect: seq = %d, want 4", f.Seq)
	}
}

// TestHandleStageLogs_SlowClientReceivesTruncation drives the hub's
// backpressure drop path: a registered client whose send buffer is full
// gets a {"control":"stream-truncated"} frame before close.
//
// To make the drop deterministic we register a client directly (small
// send buffer, no writePump draining it yet), Broadcast past capacity to
// force the hub's default-branch drop (which sets truncated + closes
// send), THEN start writePump against a real dialed conn and assert the
// client reads its buffered frames, then the truncation control frame,
// then a close. This exercises concurrent Broadcast (hub goroutine) while
// the store/Run loop runs — meaningful under -race.
func TestHandleStageLogs_SlowClientReceivesTruncation(t *testing.T) {
	h := newLogHub(t)

	const channel = "stage-logs:run-3:stage-3"

	// A bare echo-ish endpoint that upgrades, hands the conn to a Client
	// we control, registers it, but deliberately does NOT start writePump
	// until after we've forced the drop. We coordinate via channels.
	startPump := make(chan *Client, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		// Small send buffer so a handful of broadcasts overflow it.
		client := &Client{hub: h, conn: conn, send: make(chan []byte, 2), channel: channel}
		h.register <- client
		startPump <- client
		// Block the handler goroutine until the test closes the request;
		// the pumps run on their own goroutines once we start them.
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn := dial(t, url)

	client := <-startPump
	waitRegistered(t, h, channel)

	// Fill the send buffer (cap 2), then overflow it. The overflowing
	// Broadcast hits the hub's default branch → truncated + closeSend.
	for i := 0; i < 6; i++ {
		h.Broadcast(channel, []byte(`{"seq":`+strconv.Itoa(i)+`,"line":"x"}`))
	}

	// Wait until the hub has actually dropped the client (removed from the
	// channel map) so closeSend has run before we start writePump.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		_, present := h.clients[channel][client]
		h.mu.RUnlock()
		if !present {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !client.truncated.Load() {
		t.Fatal("client.truncated not set after backpressure drop")
	}

	// Now start writePump: it drains the buffered frames, then on the
	// closed channel emits the truncation control frame, then closes.
	go client.writePump()

	sawTruncation := false
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			// Close frame / EOF: stop. We must have seen truncation first.
			break
		}
		if mt != websocket.TextMessage {
			continue
		}
		var f wsFrame
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		if f.Control == "stream-truncated" {
			sawTruncation = true
			// The next read should be the close; keep reading until error.
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			continue
		}
	}
	if !sawTruncation {
		t.Fatal("did not receive stream-truncated control frame before close")
	}
}
