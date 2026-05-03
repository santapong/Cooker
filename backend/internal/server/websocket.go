package server

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Client represents a single WebSocket connection.
type Client struct {
	hub     *WebSocketHub
	conn    *websocket.Conn
	send    chan []byte
	channel string
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
					close(client.send)
				}
			}
			h.mu.Unlock()

		case msg, ok := <-inbox:
			if !ok {
				return
			}
			h.mu.RLock()
			if clients, ok := h.clients[msg.Channel]; ok {
				for client := range clients {
					select {
					case client.send <- msg.Data:
					default:
						close(client.send)
						delete(clients, client)
					}
				}
			}
			h.mu.RUnlock()
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
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}
