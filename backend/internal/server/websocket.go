package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
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
	// Empty native nonce channels are removed when their sole client leaves.
	privateChannel bool
	// cancelProducer belongs to this connection, not the HTTP handler.
	cancelProducer context.CancelFunc
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
	c.closeOnce.Do(func() {
		c.stopProducer()
		close(c.send)
	})
}

func (c *Client) stopProducer() {
	if c.cancelProducer != nil {
		c.cancelProducer()
	}
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
	done       chan struct{}
	closeOnce  sync.Once
	closeErr   error
	// Serialize publication with backend shutdown, including late writers.
	publishMu sync.Mutex
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
		done:       make(chan struct{}),
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
	defer h.Close()
	inbox := h.backend.Subscribe()
	for {
		select {
		case <-h.done:
			return
		case client := <-h.register:
			h.mu.Lock()
			select {
			case <-h.done:
				client.closeSend()
				_ = client.conn.Close()
				h.mu.Unlock()
				continue
			default:
			}
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
					if client.privateChannel && len(clients) == 0 {
						delete(h.clients, client.channel)
					}
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
						if client.privateChannel && len(clients) == 0 {
							delete(h.clients, msg.Channel)
						}
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
	h.broadcast(channel, data, false)
}

func (h *WebSocketHub) broadcast(channel string, data []byte, safeDiagnostics bool) {
	h.publishMu.Lock()
	defer h.publishMu.Unlock()
	select {
	case <-h.done:
		return
	default:
	}
	if err := h.backend.Publish(BroadcastMessage{Channel: channel, Data: data}); err != nil {
		if safeDiagnostics {
			slog.Warn("native runtime logs publication unavailable")
		} else {
			slog.Warn("ws hub: publish failed", "err", err, "channel", channel)
		}
	}
}

// Close releases hub-backend resources. Closing the backend causes
// its Subscribe() channel to close, which makes Run() return cleanly.
// Safe to call multiple times.
func (h *WebSocketHub) Close() error {
	if h == nil || h.backend == nil {
		return nil
	}
	h.closeOnce.Do(func() {
		close(h.done)
		h.mu.Lock()
		for channel, clients := range h.clients {
			for client := range clients {
				client.closeSend()
				_ = client.conn.Close()
			}
			delete(h.clients, channel)
		}
		h.mu.Unlock()
		h.publishMu.Lock()
		h.closeErr = h.backend.Close()
		h.publishMu.Unlock()
	})
	return h.closeErr
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
// producer preserves request values but owns the connection lifecycle: HTTP
// handler return must not cancel it. Disconnect, hub shutdown and backpressure
// cancellation stop it. `produce` is the RuntimeService.Logs call wired
// by the handler; it receives an io.Writer that broadcasts each line.
func (h *WebSocketHub) HandleRuntimeLogs(w http.ResponseWriter, r *http.Request, appID, serviceID string, produce func(ctx context.Context, out io.Writer) error) {
	channel := "runtime-logs:" + appID + ":" + serviceID
	h.handleRuntimeLogConnection(w, r, channel, produce, false, func(err error) {
		slog.Warn("runtime logs producer ended", "app", appID, "service", serviceID, "err", err)
	})
}

func nativeRuntimeConnectionChannel(base string) (string, error) {
	nonce, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	// The service base is an injective base64 tuple; UUID contains no colon.
	return base + ":connection:" + nonce.String(), nil
}

func (h *WebSocketHub) handleNativeRuntimeLogs(w http.ResponseWriter, r *http.Request, base string, produce func(context.Context, io.Writer) error) {
	channel, err := nativeRuntimeConnectionChannel(base)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":"runtime log session unavailable"}`)
		return
	}
	h.handleRuntimeLogConnection(w, r, channel, produce, true, func(error) {
		slog.Warn("native runtime logs producer ended")
	})
}

// App and native streams share the proven connection-owned lifecycle. Native
// streams have a private channel per connection and fixed diagnostic text.
func (h *WebSocketHub) handleRuntimeLogConnection(w http.ResponseWriter, r *http.Request, channel string, produce func(context.Context, io.Writer) error, safeDiagnostics bool, ended func(error)) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		if safeDiagnostics {
			slog.Warn("native runtime logs upgrade refused")
		} else {
			slog.Warn("websocket upgrade failed", "err", err)
		}
		return
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	client := &Client{hub: h, conn: conn, send: make(chan []byte, 256), channel: channel, cancelProducer: cancel, privateChannel: safeDiagnostics}
	if !h.registerClient(client) {
		return
	}
	go client.writePump()
	go client.readPump()
	go func() {
		defer cancel()
		writer := &hubLineWriter{hub: h, channel: channel, safeDiagnostics: safeDiagnostics}
		if err := produce(ctx, writer); err != nil {
			ended(err)
		}
	}()
}

// hubLineWriter adapts io.Writer to a hub broadcast on a fixed channel,
// one Write == one broadcast. Used to fan runtime log output to WS
// subscribers.
type hubLineWriter struct {
	hub             *WebSocketHub
	channel         string
	safeDiagnostics bool
}

func (w *hubLineWriter) Write(p []byte) (int, error) {
	// Copy: Broadcast may retain the slice past this call.
	b := make([]byte, len(p))
	copy(b, p)
	w.hub.broadcast(w.channel, b, w.safeDiagnostics)
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

	if !h.registerClient(client) {
		return
	}

	go client.writePump()
	go client.readPump()
}

func (h *WebSocketHub) registerClient(client *Client) bool {
	select {
	case h.register <- client:
		return true
	case <-h.done:
		client.closeSend()
		_ = client.conn.Close()
		return false
	}
}

func (h *WebSocketHub) unregisterClient(client *Client) {
	select {
	case h.unregister <- client:
	case <-h.done:
	}
}

func (c *Client) readPump() {
	defer func() {
		c.stopProducer()
		c.hub.unregisterClient(c)
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
		c.stopProducer()
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
