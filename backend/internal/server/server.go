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
	wsHub := NewWebSocketHub(cfg.AllowedOrigins)

	s := &Server{
		router: router,
		config: cfg,
		wsHub:  wsHub,
	}

	// CORS middleware
	router.Use(corsMiddleware(cfg.AllowedOrigins))

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

func corsMiddleware(allowed []string) gin.HandlerFunc {
	allowAll, set := originSet(allowed)
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		switch {
		case allowAll:
			c.Header("Access-Control-Allow-Origin", "*")
		case origin != "" && set[origin]:
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// originSet returns (allowAll, lookup) for the given origin list.
// A single-element list of ["*"] flags permissive mode.
func originSet(allowed []string) (bool, map[string]bool) {
	if len(allowed) == 1 && allowed[0] == "*" {
		return true, nil
	}
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o != "" {
			set[o] = true
		}
	}
	return false, set
}
