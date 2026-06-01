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
	"github.com/santapong/cooker/internal/governance"
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
	jobQueue      *jobQueueDeps
	scheduler     *schedulerDeps
	governance    *governance.Client
	// templatesDB is the dedicated *sql.DB opened by bootTemplates
	// when the jobqueue is off; nil when templates share the
	// jobqueue's pool or when the templates feature is disabled.
	// Closed in Server.Close after the WS hub.
	templatesDB  *struct{ closer func() error }
	healthCancel context.CancelFunc
	healthDone   chan struct{}
}

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
	govClient := governance.New(cfg.Governance.URL, cfg.Governance.BootstrapServices, cfg.Governance.FailOpenEnvs).
		WithCallerToken(cfg.Governance.CallerToken).
		WithDelegateToken(cfg.Governance.DelegateToken)
	if govClient.Enabled() {
		slog.Info("governance admission hook enabled",
			"url", cfg.Governance.URL,
			"fail_open_envs", cfg.Governance.FailOpenEnvs,
			"bootstrap_services", cfg.Governance.BootstrapServices,
			"caller_auth", cfg.Governance.CallerToken != "",
			"delegation", govClient.DelegationEnabled())
	}
	govDeployHook := governance.PipelineDeployHook(govClient, st, func(ctx context.Context, pipelineID string) (string, error) {
		p, err := st.Pipelines.Get(ctx, pipelineID)
		if err != nil || p == nil {
			return "", err
		}
		return p.Name, nil
	})

	exec := service.NewExecutor(
		service.WithBuilder(bld),
		service.WithPusher(selectPusher(cfg.PusherBackend)),
		service.WithDeployer(selectDeployer(cfg.DeployerBackend, cfg.Kubernetes.Kubeconfig)),
		// Docker-host per-service deploy runtimes (compose deployment DAGs
		// targeting DeployTargetDockerHost). They shell out to the local
		// docker CLI; harmless when unused.
		service.WithDockerDeployer(deployer.NewDockerRun()),
		service.WithComposeDeployer(deployer.NewCompose()),
		service.WithLogBroadcaster(wsHub.Broadcast),
		service.WithStatusBroadcaster(wsHub.Broadcast),
		service.WithDeployGovernanceHook(govDeployHook),
	)
	appDeployer := service.NewAppDeployer(exec, cfg.Registry)

	runs := NewRunCoordinator(st)

	const idempotencyMaxBytes = 32 << 20
	idem := idempotency.NewMemoryBounded(5*time.Minute, idempotencyMaxBytes)
	cleanups = append(cleanups, func() { idem.Close() })

	h := handler.New(st, codec, secMgr)
	h.AppDeployer = appDeployer
	h.WSBroadcast = wsHub.Broadcast
	h.Executor = exec
	h.Runs = runs
	// Runtime panel: inspect/tail the live container or pod backing a
	// deployed compose service. CLI-backed (docker/kubectl); harmless
	// when those binaries are absent (returns "not found").
	h.Runtime = service.NewRuntimeService(cfg.Kubernetes.Namespace)
	// HostService coordinates the PEM-bytes-to-secrets-manager
	// translation for SSH hosts. nil-safe handler-side: when secMgr
	// is nil (dev without a secrets backend), SSH host create/update
	// with a key body returns 503 — non-SSH hosts continue to work.
	h.Hosts = service.NewHostService(st, secMgr)

	// SSH remote deploy target (Thread 1 of the 2026-05 plan). Wired
	// here so it can pull the App's Host via the store and resolve
	// the private key via the host service. Registration is
	// unconditional (no env-var gate) because the per-Host config
	// supplies all the credentials.
	registerSSHDeployTarget(st, h.Hosts, &cleanups)

	// Production gate: refuse to serve if any registered SSH host
	// has sshStrictHostKey=false. The check runs here (post-store
	// boot, pre-serve) because Config.Validate can't query the DB.
	if err := cfg.ValidateSSHHosts(ctx, sshHostLister{st: st}); err != nil {
		cleanup()
		return nil, err
	}

	jobDeps, err := bootJobQueue(ctx, cfg, st, exec)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("jobqueue boot: %w", err)
	}
	cleanups = append(cleanups, jobDeps.closeAll)
	if jobDeps.Enqueuer != nil {
		h.Enqueuer = jobDeps.Enqueuer
	}
	if jobDeps.TargetStore != nil {
		h.NotificationTargets = jobDeps.TargetStore
	}

	schedDeps, err := bootScheduler(ctx, cfg, jobDeps)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("scheduler boot: %w", err)
	}
	if schedDeps != nil && schedDeps.Store != nil {
		h.Schedules = schedDeps.Store
	}

	// Phase-2 / F4 pipeline-template catalog. Returns (nil, nil, nil)
	// in dev-mode without a DB; the /templates endpoints will respond
	// 503. Shares the jobqueue's *sql.DB when available; otherwise
	// opens a small dedicated pool we have to close on shutdown.
	tplStore, tplOwnedDB, err := bootTemplates(ctx, cfg, jobDeps)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("templates boot: %w", err)
	}
	if tplStore != nil {
		h.Templates = tplStore
	}
	var templatesDBCloser *struct{ closer func() error }
	if tplOwnedDB != nil {
		db := tplOwnedDB
		templatesDBCloser = &struct{ closer func() error }{closer: db.Close}
		cleanups = append(cleanups, func() { _ = db.Close() })
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
		scheduler:     schedDeps,
		governance:    govClient,
		templatesDB:   templatesDBCloser,
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

const shutdownTimeout = 30 * time.Second

func (s *Server) RunContext(ctx context.Context, addr string) error {
	var jobPoolDone chan struct{}
	if s.jobQueue != nil && s.jobQueue.Pool != nil {
		jobPoolDone = make(chan struct{})
		go func() {
			defer close(jobPoolDone)
			_ = s.jobQueue.Pool.Run(ctx)
		}()
	}

	var schedDone chan struct{}
	if s.scheduler != nil && s.scheduler.Runner != nil {
		schedDone = make(chan struct{})
		go func() {
			defer close(schedDone)
			_ = s.scheduler.Runner.Run(ctx)
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
		if schedDone != nil {
			t := time.NewTimer(5 * time.Second)
			select {
			case <-schedDone:
			case <-t.C:
				slog.Warn("shutdown: scheduler drain timed out")
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
	if s.jobQueue != nil {
		s.jobQueue.closeAll()
	}
	if s.templatesDB != nil && s.templatesDB.closer != nil {
		_ = s.templatesDB.closer()
	}
	if s.store == nil {
		return nil
	}
	return s.store.Close()
}

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
