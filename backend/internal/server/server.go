package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cooker-ci/cooker/internal/auth"
	"github.com/cooker-ci/cooker/internal/builder"
	"github.com/cooker-ci/cooker/internal/config"
	"github.com/cooker-ci/cooker/internal/crypto"
	"github.com/cooker-ci/cooker/internal/deployer"
	"github.com/cooker-ci/cooker/internal/handler"
	"github.com/cooker-ci/cooker/internal/pusher"
	"github.com/cooker-ci/cooker/internal/service"
	"github.com/cooker-ci/cooker/internal/store"
	"github.com/cooker-ci/cooker/internal/store/memory"
	"github.com/cooker-ci/cooker/internal/store/postgres"
	"github.com/gin-gonic/gin"
)

// Server holds the HTTP server and all dependencies.
type Server struct {
	router  *gin.Engine
	config  *config.Config
	wsHub   *WebSocketHub
	oidcMW  *auth.Middleware
	handler *handler.Handler
	store   *store.Store
}

// New creates a new Server instance with all routes and middleware.
// The store is constructed from cfg.DatabaseURL: an empty URL selects
// the in-memory backend (useful for tests and development), otherwise
// Cooker connects to PostgreSQL and applies embedded migrations.
func New(cfg *config.Config) (*Server, error) {
	ctx := context.Background()

	// OIDC provider discovery happens here so misconfiguration fails fast
	// rather than showing up as 500s on the first authenticated request.
	oidcMW, err := auth.NewMiddleware(ctx, cfg.OIDC)
	if err != nil {
		return nil, err
	}

	st, err := newStore(ctx, cfg)
	if err != nil {
		return nil, err
	}

	codec, err := crypto.NewCodec(cfg.SecretKey)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("crypto: %w", err)
	}

	router := gin.Default()
	wsHub := NewWebSocketHub(cfg.AllowedOrigins)

	// Build the executor with backends chosen from config. Unknown
	// values fall back to Noop with a log line so booting never
	// fails just because an operator typo'd an env var.
	exec := service.NewExecutor(
		service.WithBuilder(selectBuilder(cfg.BuilderBackend)),
		service.WithPusher(selectPusher(cfg.PusherBackend)),
		service.WithDeployer(selectDeployer(cfg.DeployerBackend, cfg.Kubernetes.Kubeconfig)),
	)
	appDeployer := service.NewAppDeployer(exec, cfg.Registry)

	h := handler.New(st, codec)
	h.AppDeployer = appDeployer
	h.WSBroadcast = wsHub.Broadcast

	s := &Server{
		router:  router,
		config:  cfg,
		wsHub:   wsHub,
		oidcMW:  oidcMW,
		handler: h,
		store:   st,
	}

	// CORS middleware
	router.Use(corsMiddleware(cfg.AllowedOrigins))

	// Health check (unauthenticated on purpose)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "cooker"})
	})

	// Register API routes (the /api/v1 group applies OIDC middleware)
	s.registerRoutes()

	// Start WebSocket hub
	go wsHub.Run()

	return s, nil
}

// Run starts the HTTP server.
func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

// Close releases resources owned by the server (currently the store).
// Safe to call on a nil Server.
func (s *Server) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

// newStore picks a backend based on config. An empty DatabaseURL means
// the operator opted into the in-memory store; otherwise we require a
// working Postgres connection at boot.
func newStore(ctx context.Context, cfg *config.Config) (*store.Store, error) {
	if cfg.DatabaseURL == "" {
		return memory.New(), nil
	}
	st, err := postgres.NewStore(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return st, nil
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

func selectBuilder(kind string) builder.Builder {
	switch kind {
	case "docker":
		return builder.NewDockerSock()
	case "buildkit":
		return builder.NewBuildKit("")
	default:
		return builder.Noop{}
	}
}

func selectPusher(kind string) pusher.Pusher {
	switch kind {
	case "docker":
		return pusher.NewDockerSock()
	case "crane":
		return pusher.NewCrane()
	default:
		return pusher.Noop{}
	}
}

func selectDeployer(kind, kubeconfig string) deployer.Deployer {
	switch kind {
	case "kubectl":
		d := deployer.NewKubectl()
		d.Kubeconfig = kubeconfig
		return d
	case "clientgo":
		return deployer.NewClientGo(kubeconfig)
	default:
		return deployer.Noop{}
	}
}

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
