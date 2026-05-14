package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/santapong/cooker/internal/audit"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/builder"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/idempotency"
	"github.com/santapong/cooker/internal/observability"
	"github.com/santapong/cooker/internal/pusher"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/secrets/awsm"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/secrets/gcpsm"
	"github.com/santapong/cooker/internal/secrets/keepsave"
	"github.com/santapong/cooker/internal/secrets/vault"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
	"github.com/santapong/cooker/internal/store/postgres"
)

// Server holds the HTTP server and all dependencies.
type Server struct {
	router        *gin.Engine
	config        *config.Config
	wsHub         *WebSocketHub
	oidcMW        *auth.Middleware
	handler       *handler.Handler
	localAuth     *handler.LocalAuthHandler
	store         *store.Store
	wsTickets     ticketStore
	redisClient   *redis.Client
	audit         audit.Sink
	traceShutdown func(context.Context) error
	runs          *RunCoordinator
	idempotency   idempotency.Store
	// jobQueue carries the Phase-1 durable queue pool + notifier
	// dispatcher + enqueuer + its own *sql.DB / pq listener. nil
	// when COOKER_JOBQUEUE_ENABLED=false (default); when present,
	// RunContext spawns Pool.Run in a tracked goroutine and the
	// shutdown branch drains it deterministically.
	jobQueue *jobQueueDeps
	// healthCancel cancels the AppHealthChecker goroutine on shutdown.
	// nil means the checker was disabled (interval <= 0) or boot
	// failed before the checker started. healthDone is closed by the
	// checker goroutine when it returns; RunContext waits on it
	// before returning so the drain order is deterministic.
	healthCancel context.CancelFunc
	healthDone   chan struct{}
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

	var localAuthHandler *handler.LocalAuthHandler
	if cfg.LocalAuth.Enabled {
		key, err := config.DecodeLocalAuthSigningKey(cfg.LocalAuth.JWTSigningKey)
		if err != nil {
			st.Close()
			return nil, fmt.Errorf("local auth: %w", err)
		}
		issuer, err := local.NewIssuer(key, cfg.LocalAuth.TokenTTL)
		if err != nil {
			st.Close()
			return nil, fmt.Errorf("local auth issuer: %w", err)
		}
		oidcMW.EnableLocalAuth(issuer)
		localAuthHandler = handler.NewLocalAuthHandler(st.Users, issuer, cfg.LocalAuth.AllowSignup)
		slog.Info("local auth enabled", "allow_signup", cfg.LocalAuth.AllowSignup, "token_ttl", cfg.LocalAuth.TokenTTL.String())
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

	if cfg.Env != config.EnvDev {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())

	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	cleanups = append(cleanups, func() { st.Close() })

	var redisClient *redis.Client
	if cfg.WSTicket.Backend == "redis" || cfg.RateLimit.Backend == "redis" || cfg.WSHub.Backend == "redis" {
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("redis: parse url: %w", err)
		}
		redisClient = redis.NewClient(opts)
		cleanups = append(cleanups, func() { _ = redisClient.Close() })
	}

	var wsHub *WebSocketHub
	switch cfg.WSHub.Backend {
	case "redis":
		if redisClient == nil {
			cleanup()
			return nil, fmt.Errorf("ws hub backend=redis requires REDIS_URL")
		}
		backend, err := newRedisHubBackend(ctx, redisClient)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("ws hub redis backend: %w", err)
		}
		wsHub = NewWebSocketHubWithBackend(cfg.AllowedOrigins, backend)
	default:
		wsHub = NewWebSocketHub(cfg.AllowedOrigins)
	}
	cleanups = append(cleanups, func() { _ = wsHub.Close() })

	var wsTickets ticketStore
	switch cfg.WSTicket.Backend {
	case "redis":
		wsTickets = newRedisTicketStore(redisClient, 60*time.Second)
	default:
		wsTickets = newWSTicketStore(60 * time.Second)
	}

	traceShutdown, err := observability.Setup(ctx, observability.Config{
		MetricsEnabled: cfg.Observability.MetricsEnabled,
		TracingEnabled: cfg.Observability.TracingEnabled,
		OTLPEndpoint:   cfg.Observability.OTLPEndpoint,
		OTLPInsecure:   cfg.Observability.OTLPInsecure,
		ServiceName:    cfg.Observability.ServiceName,
		ServiceVersion: cfg.Observability.ServiceVersion,
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("observability: %w", err)
	}
	if traceShutdown != nil {
		cleanups = append(cleanups, func() {
			c, cancelTrace := context.WithTimeout(context.Background(), 2*time.Second)
			_ = traceShutdown(c)
			cancelTrace()
		})
	}

	auditSink, err := newAuditSink(cfg.Audit)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("audit: %w", err)
	}
	if auditSink != nil {
		cleanups = append(cleanups, func() { _ = auditSink.Close() })
	}

	bld, err := selectBuilder(cfg.BuilderBackend, cfg.Kubernetes)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("builder: %w", err)
	}
	exec := service.NewExecutor(
		service.WithBuilder(bld),
		service.WithPusher(selectPusher(cfg.PusherBackend)),
		service.WithDeployer(selectDeployer(cfg.DeployerBackend, cfg.Kubernetes.Kubeconfig)),
		service.WithLogBroadcaster(wsHub.Broadcast),
	)
	appDeployer := service.NewAppDeployer(exec, cfg.Registry)

	runs := NewRunCoordinator(st)

	const idempotencyMaxBytes = 32 << 20 // 32 MiB
	idem := idempotency.NewMemoryBounded(5*time.Minute, idempotencyMaxBytes)
	cleanups = append(cleanups, func() { idem.Close() })

	h := handler.New(st, codec, secMgr)
	h.AppDeployer = appDeployer
	h.WSBroadcast = wsHub.Broadcast
	h.Executor = exec
	h.Runs = runs

	// Phase-1 / A1 durable job queue. When COOKER_JOBQUEUE_ENABLED=
	// false (default), bootJobQueue returns deps with all fields nil
	// and the handler's Enqueuer stays nil — RunPipeline keeps using
	// the inline Runs.Spawn path (pre-Phase-1 behaviour). When true,
	// pipeline-run jobs go through the durable queue.
	jobDeps, err := bootJobQueue(ctx, cfg, st, exec)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("jobqueue boot: %w", err)
	}
	cleanups = append(cleanups, jobDeps.closeAll)
	if jobDeps.Enqueuer != nil {
		h.Enqueuer = jobDeps.Enqueuer
	}

	sweepCtx, sweepCancel := context.WithTimeout(ctx, 5*time.Second)
	if n, err := st.Runs.SweepOrphans(sweepCtx, orphanThreshold); err != nil {
		slog.Warn("orphan sweep failed", "err", err)
	} else if n > 0 {
		slog.Info("orphan sweep: marked stale runs failed", "count", n)
		observability.AddPipelineRunsOrphaned(n)
	}
	sweepCancel()

	var healthCancel context.CancelFunc
	var healthDone chan struct{}
	if cfg.AppHealthInterval > 0 {
		healthCtx, cancel := context.WithCancel(context.Background())
		healthCancel = cancel
		healthDone = make(chan struct{})
		checker := service.NewAppHealthChecker(st.Apps,
			service.WithAppHealthInterval(cfg.AppHealthInterval),
		)
		go func() {
			defer close(healthDone)
			_ = checker.Run(healthCtx)
		}()
		cleanups = append(cleanups, func() {
			if healthCancel != nil {
				healthCancel()
			}
			if healthDone != nil {
				select {
				case <-healthDone:
				case <-time.After(2 * time.Second):
				}
			}
		})
	}

	s := &Server{
		router:        router,
		config:        cfg,
		wsHub:         wsHub,
		oidcMW:        oidcMW,
		handler:       h,
		localAuth:     localAuthHandler,
		store:         st,
		wsTickets:     wsTickets,
		audit:         auditSink,
		traceShutdown: traceShutdown,
		redisClient:   redisClient,
		runs:          runs,
		idempotency:   idem,
		jobQueue:      jobDeps,
		healthCancel:  healthCancel,
		healthDone:    healthDone,
	}

	router.Use(securityHeadersMiddleware())
	router.Use(corsMiddleware(cfg.AllowedOrigins))
	if cfg.Observability.MetricsEnabled {
		router.Use(observability.MetricsMiddleware())
		router.GET("/metrics", observability.MetricsHandler())
	}
	if cfg.Observability.TracingEnabled {
		router.Use(observability.TracingMiddleware(cfg.Observability.ServiceName))
	}
	live := livenessHandler()
	var jwksAge func() (time.Duration, bool)
	if cfg.OIDC.Enabled && oidcMW != nil {
		jwksAge = oidcMW.LastJWKSRefresh
	}
	ready := readinessHandler(st, redisClient, jwksAge)
	router.GET("/health", live)
	router.GET("/health/live", live)
	router.GET("/health/ready", ready)
	router.GET("/version", versionHandler())

	registerDeployTargets(cfg.DeployTargets)
	s.registerRoutes()
	go wsHub.Run()
	return s, nil
}

// shutdownTimeout is how long Shutdown waits for in-flight HTTP
// requests to finish before forcing connections closed.
const shutdownTimeout = 30 * time.Second

// RunContext runs the HTTP server until ctx is cancelled (e.g. SIGTERM),
// then drains in-flight requests with shutdownTimeout before returning.
// Returns the first error from ListenAndServe or Shutdown, or nil on a
// clean drain.
func (s *Server) RunContext(ctx context.Context, addr string) error {
	// Job queue pool runs alongside the HTTP server. When ctx cancels,
	// Pool.Run sees the cancellation and exits when its workers'
	// current jobs finish (graceful by design). jobPoolDone is closed
	// once Pool.Run returns so the shutdown branch can wait on it.
	var jobPoolDone chan struct{}
	if s.jobQueue != nil && s.jobQueue.Pool != nil {
		jobPoolDone = make(chan struct{})
		go func() {
			defer close(jobPoolDone)
			_ = s.jobQueue.Pool.Run(ctx)
		}()
	}

	httpSrv := &http.Server{Addr: addr, Handler: s.router}
	errCh := make(chan error, 1)
	go func() {
		err := httpSrv.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// Shutdown drain order (W10-13, extended for jobqueue):
		// 1. HTTP drain
		// 2. run coordinator drain
		// 3. job queue pool drain (workers complete in-flight jobs)
		// 4. health checker cancel + 5s wait
		slog.Info("shutdown: draining HTTP server", "timeout", shutdownTimeout.String())
		drainCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpSrv.Shutdown(drainCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		if s.runs != nil {
			runDrainCtx, runCancel := context.WithTimeout(context.Background(), runDrainTimeout)
			s.runs.Wait(runDrainCtx)
			runCancel()
		}
		if jobPoolDone != nil {
			t := time.NewTimer(runDrainTimeout)
			select {
			case <-jobPoolDone:
			case <-t.C:
				slog.Warn("shutdown: jobqueue pool drain timed out")
			}
			t.Stop()
		}
		if s.healthCancel != nil {
			s.healthCancel()
		}
		if s.healthDone != nil {
			t := time.NewTimer(5 * time.Second)
			defer t.Stop()
			select {
			case <-s.healthDone:
			case <-t.C:
				slog.Warn("shutdown: app health checker drain timed out")
			}
		}
		return <-errCh
	}
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	if s.traceShutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.traceShutdown(shutdownCtx)
		cancel()
	}
	if s.audit != nil {
		_ = s.audit.Close()
	}
	if s.wsHub != nil {
		_ = s.wsHub.Close()
	}
	if s.redisClient != nil {
		_ = s.redisClient.Close()
	}
	// Job queue resources (listener + dedicated *sql.DB) close after
	// the WS hub so any final stage-log broadcasts produced by
	// in-flight Execute calls have already flushed.
	if s.jobQueue != nil {
		s.jobQueue.closeAll()
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
	case "buildah":
		return builder.NewBuildah(builder.BuildahConfig{
			Kubeconfig:     k8s.Kubeconfig,
			Namespace:      k8s.Namespace,
			Image:          k8s.BuildahImage,
			ServiceAccount: k8s.BuildahServiceAccount,
			ContextPVC:     k8s.BuildahContextPVC,
			StorageDriver:  k8s.BuildahStorageDriver,
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
	ctx := context.Background()
	switch cfg.SecretsBackend {
	case "keepsave":
		if cfg.KeepSave.URL == "" || cfg.KeepSave.ProjectID == "" || cfg.KeepSave.APIKey == "" {
			return nil, fmt.Errorf("keepsave backend requires COOKER_SECRETS_KEEPSAVE_URL, _PROJECT_ID, _API_KEY")
		}
		client := keepsave.NewClient(cfg.KeepSave.URL, cfg.KeepSave.APIKey)
		slog.Info("secrets backend selected", "backend", "keepsave", "url", cfg.KeepSave.URL, "project", cfg.KeepSave.ProjectID)
		return keepsave.New(client, st.Environments, cfg.KeepSave.ProjectID), nil
	case "vault":
		if cfg.Vault.Addr == "" {
			return nil, fmt.Errorf("vault backend requires COOKER_SECRETS_VAULT_ADDR")
		}
		slog.Info("secrets backend selected", "backend", "vault", "addr", cfg.Vault.Addr, "mount", cfg.Vault.Mount)
		return vault.New(vault.Config{
			Address: cfg.Vault.Addr,
			Token:   cfg.Vault.Token,
			Mount:   cfg.Vault.Mount,
			Prefix:  cfg.Vault.Prefix,
		})
	case "aws":
		slog.Info("secrets backend selected", "backend", "aws-secrets-manager", "region", cfg.AWSSecrets.Region, "prefix", cfg.AWSSecrets.Prefix)
		return awsm.New(ctx, awsm.Config{
			Region: cfg.AWSSecrets.Region,
			Prefix: cfg.AWSSecrets.Prefix,
		})
	case "gcp":
		if cfg.GCPSecrets.ProjectID == "" {
			return nil, fmt.Errorf("gcp backend requires COOKER_SECRETS_GCP_PROJECT_ID")
		}
		slog.Info("secrets backend selected", "backend", "gcp-secret-manager", "project", cfg.GCPSecrets.ProjectID)
		return gcpsm.New(ctx, gcpsm.Config{
			ProjectID: cfg.GCPSecrets.ProjectID,
			Prefix:    cfg.GCPSecrets.Prefix,
		})
	case "database", "":
		if codec == nil || !codec.Active() {
			slog.Warn("secrets backend selected", "backend", "database", "codec", "inactive", "note", "secret endpoints will return 503 until COOKER_SECRET_KEY is set")
			return nil, nil
		}
		slog.Info("secrets backend selected", "backend", "database", "codec", "AES-GCM")
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
