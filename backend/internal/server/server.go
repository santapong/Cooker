package server

import (
	"net/http"

	"github.com/cooker-ci/cooker/internal/config"
	"github.com/gin-gonic/gin"
)

// Server holds the HTTP server and all dependencies.
type Server struct {
	router *gin.Engine
	config *config.Config
	wsHub  *WebSocketHub
}

// New creates a new Server instance with all routes and middleware.
func New(cfg *config.Config) (*Server, error) {
	router := gin.Default()
	wsHub := NewWebSocketHub()

	s := &Server{
		router: router,
		config: cfg,
		wsHub:  wsHub,
	}

	// CORS middleware
	router.Use(corsMiddleware())

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "cooker"})
	})

	// Register API routes
	s.registerRoutes()

	// Start WebSocket hub
	go wsHub.Run()

	return s, nil
}

// Run starts the HTTP server.
func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
