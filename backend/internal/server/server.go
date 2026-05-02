package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/cooker-ci/cooker/internal/audit"
	"github.com/cooker-ci/cooker/internal/auth"
	"github.com/cooker-ci/cooker/internal/builder"
	"github.com/cooker-ci/cooker/internal/config"
	"github.com/cooker-ci/cooker/internal/crypto"
	"github.com/cooker-ci/cooker/internal/deployer"
	"github.com/cooker-ci/cooker/internal/handler"
	"github.com/cooker-ci/cooker/internal/pusher"
	"github.com/cooker-ci/cooker/internal/secrets"
	"github.com/cooker-ci/cooker/internal/secrets/database"
	"github.com/cooker-ci/cooker/internal/secrets/keepsave"
	"github.com/cooker-ci/cooker/internal/service"
	"github.com/cooker-ci/cooker/internal/store"
	"github.com/cooker-ci/cooker/internal/store/memory"
	"github.com/cooker-ci/cooker/internal/store/postgres"
	"github.com/gin-gonic/gin"
)

// Server holds the HTTP server and all dependencies.
type Server struct {
	router    *gin.Engine
	config    *config.Config
	wsHub     *WebSocketHub
	oidcMW    *auth.Middleware
	handler   *handler.Handler
	store     *store.Store
	wsTickets *wsTicketStore
	audit     audit.Sink
}

// New creates a new Server instance with all routes and middleware.
func New(cfg *config.Config) (*Server, error) {
	ctx := context.Background()

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

	secMgr, err := selectSecretsManager(cfg, st, codec)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("secrets: %w", err)
	}

	router := gin.Default()
	wsHub := NewWebSocketHub(cfg.AllowedOrigins)
	wsTickets := newWSTicketStore(60 * time.Second)

	auditSink, err := newAuditSink(cfg.Audit)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("audit: %w", err)
	}

	bld, err := selectBuilder(cfg.BuilderBackend, cfg.Kubernetes)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("builder: %w", err)
	}
	exec := service.NewExecutor(
		service.WithBuilder(bld),
		service.WithPusher(selectPusher(cfg.PusherBackend)),
		service.WithDeployer(selectDeployer(cfg.DeployerBackend, cfg.Kubernetes.Kubeconfig)),
	)
	appDeployer := service.NewAppDeployer(exec, cfg.Registry)

	h := handler.New(st, codec, secMgr)
	h.AppDeployer = appDeployer
	h.WSBroadcast = wsHub.Broadcast

	s := &Server{
		router:    router,
		config:    cfg,
		wsHub:     wsHub,
		oidcMW:    oidcMW,
		handler:   h,
		store:     st,
		wsTickets: wsTickets,
		audit:     auditSink,
	}

	router.Use(corsMiddleware(cfg.AllowedOrigins))
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "cooker"})
	})

	s.registerRoutes()
	go wsHub.Run()
	return s, nil
}

func (s *Server) Run(addr string) error { return s.router.Run(addr) }

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	if s.audit != nil {
		_ = s.audit.Close()
	}
	if s.store == nil {
		return nil
	}
	return s.store.Close()
}

// newAuditSink builds the configured audit sink, or returns nil when
// auditing is disabled. A nil Sink causes auditMiddleware to act as a
// passthrough — which is what dev-mode wants by default.
func newAuditSink(cfg config.AuditConfig) (audit.Sink, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	switch cfg.Destination {
	case "", "stdout":
		return audit.NewStdoutSink(nil), nil
	case "file":
		return audit.NewFileSink(cfg.FilePath)
	default:
		return nil, fmt.Errorf("unknown destination %q", cfg.Destination)
	}
}

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
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func selectBuilder(kind string, k8s config.KubernetesConfig) (builder.Builder, error) {
	switch kind {
	case "docker":
		return builder.NewDockerSock(), nil
	case "buildkit":
		return builder.NewBuildKit(""), nil
	case "kaniko":
		return builder.NewKaniko(builder.KanikoConfig{
			Kubeconfig:     k8s.Kubeconfig,
			Namespace:      k8s.Namespace,
			Image:          k8s.KanikoImage,
			ServiceAccount: k8s.KanikoServiceAccount,
			ContextPVC:     k8s.KanikoContextPVC,
		})
	default:
		return builder.Noop{}, nil
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

// selectSecretsManager constructs the configured Manager. Returning
// (nil, nil) is intentional for the dev-mode "database backend with
// no key configured" case: server boots with secret endpoints gated
// by Handler.requireSecrets returning 503 — same observable behavior
// as pre-Manager Cooker.
func selectSecretsManager(cfg *config.Config, st *store.Store, codec *crypto.Codec) (secrets.Manager, error) {
	switch cfg.SecretsBackend {
	case "keepsave":
		if cfg.KeepSave.URL == "" || cfg.KeepSave.ProjectID == "" || cfg.KeepSave.APIKey == "" {
			return nil, fmt.Errorf("keepsave backend requires COOKER_SECRETS_KEEPSAVE_URL, _PROJECT_ID, _API_KEY")
		}
		client := keepsave.NewClient(cfg.KeepSave.URL, cfg.KeepSave.APIKey)
		log.Printf("secrets: backend=keepsave url=%s project=%s", cfg.KeepSave.URL, cfg.KeepSave.ProjectID)
		return keepsave.New(client, st.Environments, cfg.KeepSave.ProjectID), nil
	case "database", "":
		if codec == nil || !codec.Active() {
			log.Printf("secrets: backend=database (codec inactive — secret endpoints will return 503 until COOKER_SECRET_KEY is set)")
			return nil, nil
		}
		log.Printf("secrets: backend=database (AES-GCM)")
		return database.New(st.Environments, codec), nil
	default:
		return nil, fmt.Errorf("unknown secrets backend %q", cfg.SecretsBackend)
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
