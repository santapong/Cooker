package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/santapong/cooker/internal/logstore"
)

// WebSocket idle / ping-pong tunables. pongWait should be larger
// than pingPeriod with a comfortable margin so a single dropped ping
// doesn't immediately drop the client. writeWait caps how long a
// single WriteMessage blocks before we treat the client as dead.
const (
	wsPongWait   = 60 * time.Second
	wsPingPeriod = (wsPongWait * 9) / 10
	wsWriteWait  = 10 * time.Second
)

// Client represents a single WebSocket connection.
type Client struct {
	hub     *WebSocketHub
	conn    *websocket.Conn
	send    chan []byte
	channel string
	// closeOnce guards close(send) so it can fire from at most one
	// path (broadcast-backpressure or unregister) without panicking.
	closeOnce sync.Once
	// truncated is set by the hub's drop path (slow client, send buffer
	// full) before it closes send, so writePump can emit one final
	// {"control":"stream-truncated"} frame instead of a silent close. It
	// is NOT set on a clean unregister. Read in writePump (a different
	// goroutine than the hub), so it must be atomic.
	truncated atomic.Bool
}

// closeSend is the single-path-safe close on the send channel.
// Multiple callers may race (broadcast loop saw the channel full
// and decided to drop the client; the hub also processes an
// unregister from readPump/writePump exit) — sync.Once ensures
// only one wins.
func (c *Client) closeSend() {
	c.closeOnce.Do(func() { close(c.send) })
}

// WebSocketHub manages all WebSocket connections and message broadcasting.
// Per-client subscription state (the clients map) is per-process; broadcast
// fan-out can be cross-replica via a Redis HubBackend.
type WebSocketHub struct {
	upgrader   websocket.Upgrader
	mu         sync.RWMutex
	clients    map[string]map[*Client]bool // channel -> clients
	backend    HubBackend
	register   chan *Client
	unregister chan *Client
	// logStore, when set, supplies the per-stage backlog replayed on
	// connect by HandleStageLogs (?since=<seq>). nil disables replay so
	// the handler degrades to live-only streaming (back-compat).
	logStore logstore.Store
}

// BroadcastMessage is a message sent to a specific channel. Tagged for
// json so the Redis backend can ship it across the wire.
type BroadcastMessage struct {
	Channel string `json:"channel"`
	Data    []byte `json:"data"`
}

// NewWebSocketHub creates a new hub backed by the in-process channel.
// allowedOrigins controls which Origin headers are accepted on the
// WebSocket upgrade; ["*"] means allow any.
func NewWebSocketHub(allowedOrigins []string) *WebSocketHub {
	return NewWebSocketHubWithBackend(allowedOrigins, newMemoryHubBackend())
}

// NewWebSocketHubWithBackend builds a hub with a caller-supplied
// backend. Used by server.New to swap in the Redis pub/sub backend
// when COOKER_WS_HUB_BACKEND=redis.
func NewWebSocketHubWithBackend(allowedOrigins []string, backend HubBackend) *WebSocketHub {
	allowAll, set := originSet(allowedOrigins)
	return &WebSocketHub{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				if allowAll {
					return true
				}
				origin := r.Header.Get("Origin")
				if origin == "" {
					return false
				}
				return set[origin]
			},
		},
		clients:    make(map[string]map[*Client]bool),
		backend:    backend,
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// SetLogStore installs the per-stage log history used by HandleStageLogs
// for replay-on-connect. Call once during server construction, before the
// hub serves traffic; nil leaves replay disabled. Not safe to call
// concurrently with HandleStageLogs.
func (h *WebSocketHub) SetLogStore(s logstore.Store) {
	h.logStore = s
}

// Run processes WebSocket hub events. Reads broadcasts from the
// backend's Subscribe channel (memory or Redis).
func (h *WebSocketHub) Run() {
	inbox := h.backend.Subscribe()
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if _, ok := h.clients[client.channel]; !ok {
				h.clients[client.channel] = make(map[*Client]bool)
			}
			h.clients[client.channel][client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.clients[client.channel]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					client.closeSend()
				}
			}
			h.mu.Unlock()

		case msg, ok := <-inbox:
			if !ok {
				return
			}
			// Two-pass to avoid mutating the map while iterating
			// under RLock: collect victims first, then upgrade to
			// Lock to delete + close. Go panics on concurrent
			// map-write-during-iterate.
			var dropped []*Client
			h.mu.RLock()
			if clients, ok := h.clients[msg.Channel]; ok {
				for client := range clients {
					select {
					case client.send <- msg.Data:
					default:
						dropped = append(dropped, client)
					}
				}
			}
			h.mu.RUnlock()
			if len(dropped) > 0 {
				h.mu.Lock()
				if clients, ok := h.clients[msg.Channel]; ok {
					for _, client := range dropped {
						delete(clients, client)
						// Slow client: flag truncation so writePump emits a
						// {"control":"stream-truncated"} frame before the
						// CloseMessage. This is the backpressure-drop path
						// ONLY — a clean unregister (above) leaves the flag
						// unset so a normal disconnect stays silent.
						client.truncated.Store(true)
						client.closeSend()
					}
				}
				h.mu.Unlock()
			}
		}
	}
}

// Broadcast sends a message to all clients on a channel across every
// replica, depending on the configured backend.
func (h *WebSocketHub) Broadcast(channel string, data []byte) {
	if err := h.backend.Publish(BroadcastMessage{Channel: channel, Data: data}); err != nil {
		slog.Warn("ws hub: publish failed", "err", err, "channel", channel)
	}
}

// Close releases hub-backend resources. Closing the backend causes
// its Subscribe() channel to close, which makes Run() return cleanly.
// Safe to call multiple times.
func (h *WebSocketHub) Close() error {
	if h == nil || h.backend == nil {
		return nil
	}
	return h.backend.Close()
}

// HandlePipelineRun handles WebSocket connections for pipeline run updates.
func (h *WebSocketHub) HandlePipelineRun(w http.ResponseWriter, r *http.Request, runID string) {
	h.handleConnection(w, r, "pipeline-run:"+runID)
}

// HandleDockerBuild handles WebSocket connections for Docker build streams.
func (h *WebSocketHub) HandleDockerBuild(w http.ResponseWriter, r *http.Request, buildID string) {
	h.handleConnection(w, r, "docker-build:"+buildID)
}

// HandleKubeWatch handles WebSocket connections for Kubernetes watch events.
func (h *WebSocketHub) HandleKubeWatch(w http.ResponseWriter, r *http.Request, namespace, resource string) {
	channel := "kube-watch:" + namespace + ":" + resource
	h.handleConnection(w, r, channel)
}

// HandleRuntimeLogs upgrades the connection, subscribes it to the
// runtime-logs channel for (appID, serviceID), and starts a producer
// goroutine that tails the live container/pod logs into the hub. The
// producer is bound to the request context, so it stops when the
// client disconnects. `produce` is the RuntimeService.Logs call wired
// by the handler; it receives an io.Writer that broadcasts each line.
func (h *WebSocketHub) HandleRuntimeLogs(w http.ResponseWriter, r *http.Request, appID, serviceID string, produce func(ctx context.Context, out io.Writer) error) {
	channel := "runtime-logs:" + appID + ":" + serviceID
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}
	client := &Client{hub: h, conn: conn, send: make(chan []byte, 256), channel: channel}
	h.register <- client
	go client.writePump()
	go client.readPump()

	// Producer: tail logs into the hub on this channel until the client
	// (request context) goes away. Runs in its own goroutine so the
	// handler returns and the WS pumps stay live.
	go func() {
		ctx := r.Context()
		writer := &hubLineWriter{hub: h, channel: channel}
		if perr := produce(ctx, writer); perr != nil {
			slog.Warn("runtime logs producer ended", "app", appID, "service", serviceID, "err", perr)
		}
	}()
}

// hubLineWriter adapts io.Writer to a hub broadcast on a fixed channel,
// one Write == one broadcast. Used to fan runtime log output to WS
// subscribers.
type hubLineWriter struct {
	hub     *WebSocketHub
	channel string
}

func (w *hubLineWriter) Write(p []byte) (int, error) {
	// Copy: Broadcast may retain the slice past this call.
	b := make([]byte, len(p))
	copy(b, p)
	w.hub.Broadcast(w.channel, b)
	return len(p), nil
}

func (h *WebSocketHub) handleConnection(w http.ResponseWriter, r *http.Request, channel string) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}

	client := &Client{
		hub:     h,
		conn:    conn,
		send:    make(chan []byte, 256),
		channel: channel,
	}

	h.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	// Stalled / silent connections (Slow-Loris pattern) can pin a
	// goroutine and a file descriptor indefinitely. Set a read
	// deadline that the pong handler refreshes; if no pong arrives
	// within wsPongWait, ReadMessage returns and we unregister.
	_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				// Hub closed our send channel. If this was the
				// backpressure-drop path the hub set truncated; emit one
				// final control frame so the UI can show "logs truncated —
				// reload" instead of silently missing lines. Reuses the
				// write deadline already set above (single conn-writer —
				// writePump is the only goroutine writing this conn).
				if c.truncated.Load() {
					_ = c.conn.WriteMessage(websocket.TextMessage, []byte(`{"control":"stream-truncated"}`))
					_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				}
				// Politely close rather than letting the proxy time us out.
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
