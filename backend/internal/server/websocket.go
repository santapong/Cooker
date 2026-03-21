package server

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// Client represents a single WebSocket connection.
type Client struct {
	hub     *WebSocketHub
	conn    *websocket.Conn
	send    chan []byte
	channel string
}

// WebSocketHub manages all WebSocket connections and message broadcasting.
type WebSocketHub struct {
	mu         sync.RWMutex
	clients    map[string]map[*Client]bool // channel -> clients
	broadcast  chan BroadcastMessage
	register   chan *Client
	unregister chan *Client
}

// BroadcastMessage is a message sent to a specific channel.
type BroadcastMessage struct {
	Channel string
	Data    []byte
}

// NewWebSocketHub creates a new hub.
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[string]map[*Client]bool),
		broadcast:  make(chan BroadcastMessage, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run processes WebSocket hub events.
func (h *WebSocketHub) Run() {
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

		case msg := <-h.broadcast:
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

// Broadcast sends a message to all clients on a channel.
func (h *WebSocketHub) Broadcast(channel string, data []byte) {
	h.broadcast <- BroadcastMessage{Channel: channel, Data: data}
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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
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
