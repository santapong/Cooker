package server

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/santapong/cooker/internal/audit"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/governance"
	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/idempotency"
	"github.com/santapong/cooker/internal/kube"
	"github.com/santapong/cooker/internal/license"
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/observability"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/triage"
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
	templatesDB      *struct{ closer func() error }
	healthCancel     context.CancelFunc
	healthDone       chan struct{}
	auditSweepCancel context.CancelFunc
	auditSweepDone   chan struct{}
	// rateLimiter is the in-memory per-user limiter created by
	// registerRoutes; held here so Close() can stop its gc goroutine
	// (BC-H1). nil when the redis backend or disabled limiter is used.
	rateLimiter *rateLimiter
	// webhookRateLimiter is the dedicated in-memory per-IP limiter for the
	// unauthenticated /webhooks/* receivers (C-webhook-logs). Held here so
	// Close() can stop its gc goroutine. nil when disabled.
	webhookRateLimiter *rateLimiter
	// localAuthRateLimiter is the dedicated in-memory per-IP limiter for the
	// unauthenticated local-auth signup/signin endpoints (CWE-307). Held here
	// so Close() can stop its gc goroutine. nil when local auth is disabled.
	localAuthRateLimiter *rateLimiter
}

// registerMetricsRoute mounts /metrics on the main app router ONLY when no
// dedicated metrics port is configured (metricsPort == 0). With a dedicated
// port (> 0) the scrape endpoint lives on its own listener (see RunContext)
// and must NOT be reachable via the public app ingress (audit finding
// M0-1). Extracted so tests can assert the gating without booting a full
// Server.
func registerMetricsRoute(router *gin.Engine, metricsPort int) {
	if metricsPort == 0 {
		router.GET("/metrics", observability.MetricsHandler())
	}
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
	// Do not trust any proxy headers (X-Forwarded-For) by default.
	// This prevents a remote client from spoofing its IP for rate-limit
	// bypass (M-2). Operators who run behind a trusted proxy can set
	// COOKER_TRUSTED_PROXIES (comma-separated CIDRs) — not yet exposed
	// in config, so we unconditionally disable XFF trust here.
	_ = router.SetTrustedProxies(nil)
	router.Use(gin.Recovery())

	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	cleanups = append(cleanups, func() { st.Close() })

	// Always-on notification plumbing (targets + dispatcher). Booted
	// before the services so the run/deploy/canary paths can emit
	// events regardless of whether the job queue is enabled.
	notifDeps, err := bootNotifier(ctx, cfg, codec)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("notifier boot: %w", err)
	}
	cleanups = append(cleanups, notifDeps.closeAll)

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

	// Per-stage log history for replay-on-connect (execution-observability
	// redesign Part A). Single instance shared by the hub (serves the
	// backlog on WS connect) and the executor (appends stamped lines).
	// Defaults to the in-memory backend; single-replica only, like the
	// in-memory WS hub and rate limiter.
	logStore := logstore.FromEnv()
	wsHub.SetLogStore(logStore)

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

	auditSink, err := newAuditSink(cfg.Audit, st)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("audit: %w", err)
	}
	if auditSink != nil {
		cleanups = append(cleanups, func() { _ = auditSink.Close() })
	}
	if auditSink != nil && auditHasDB(cfg.Audit.Destination) && cfg.DatabaseURL == "" {
		slog.Warn("audit: destination \"db\" is using the in-memory store; " +
			"events are capped at ~10k and lost on restart — set DATABASE_URL for a durable trail")
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
	// X-partial-rollout: make it loud when governance is wired in a way
	// that silently degrades the deploy gate to ALLOW. `configured`
	// signals operator intent (a URL or a token is present) so a
	// completely-unconfigured dev boot stays quiet; the classification
	// itself lives in governance.Client.InertReason so it's unit-tested.
	govConfigured := cfg.Governance.URL != "" ||
		cfg.Governance.CallerToken != "" ||
		cfg.Governance.DelegateToken != ""
	if reason, inert := govClient.InertReason(govConfigured, cfg.Env.IsProduction()); inert {
		slog.Warn("governance deploy gate is INERT — deploys are NOT being governed",
			"reason", reason,
			"url_set", cfg.Governance.URL != "",
			"caller_token_set", cfg.Governance.CallerToken != "",
			"delegate_token_set", cfg.Governance.DelegateToken != "",
			"env", cfg.Env)
	}
	govDeployHook := governance.PipelineDeployHook(govClient, st, func(ctx context.Context, pipelineID string) (string, error) {
		p, err := st.Pipelines.Get(ctx, pipelineID)
		if err != nil || p == nil {
			return "", err
		}
		return p.Name, nil
	})

	stageRunner, err := selectStageRunner(cfg.StageRunner, cfg.Kubernetes)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("stage runner: %w", err)
	}
	// Approval-gate persistence for StageTypeApproval stages (HS26-05-03).
	// The executor opens + polls the gate; the stage approve/reject handlers
	// resolve it. Both share this one service over the same store.
	stageApprovals := service.NewStageApprovalService(st.StageApprovals)

	// Hoisted so it can be shared between the executor (deploy stages) and
	// the canary service (weighted traffic split). Type-asserted below for
	// the optional WeightedDeployer capability.
	deploy := selectDeployer(cfg.DeployerBackend, cfg.Kubernetes.Kubeconfig)
	// Deployed-app reverse proxy / URL surface (COOKER_PROXY_*). Passed
	// to both the executor (docker-run Traefik labels) and the app
	// deployer (ProxyHost stamping, Ingress synthesis, DeployedURL).
	svcProxy := service.ProxyConfig{
		Domain:       cfg.Proxy.Domain,
		Scheme:       cfg.Proxy.Scheme,
		IngressClass: cfg.Proxy.IngressClass,
		Network:      cfg.Proxy.Network,
	}
	exec := service.NewExecutor(
		service.WithBuilder(bld),
		service.WithPusher(selectPusher(cfg.PusherBackend)),
		service.WithDeployer(deploy),
		// Docker-host per-service deploy runtimes (compose deployment DAGs
		// targeting DeployTargetDockerHost). They shell out to the local
		// docker CLI; harmless when unused.
		service.WithDockerDeployer(deployer.NewDockerRun()),
		service.WithComposeDeployer(deployer.NewCompose()),
		// Test/Custom stages run user code in an isolated container
		// (kube Job / docker run / noop), selected by COOKER_STAGE_RUNNER.
		service.WithStageRunner(stageRunner),
		service.WithStageApprovals(stageApprovals),
		service.WithLogBroadcaster(wsHub.Broadcast),
		service.WithLogStore(logStore),
		service.WithStatusBroadcaster(wsHub.Broadcast),
		service.WithDeployGovernanceHook(govDeployHook),
		service.WithProxyConfig(svcProxy),
		// Mid-run progress persistence: the executor's batched drain
		// flushes stage transitions through the cheap single-column
		// UpdateProgress (logs stripped; they land in the terminal
		// Update). Previously no updater was wired, so mid-run progress
		// was never durable and a crash showed stages as pending.
		service.WithRunUpdater(func(ctx context.Context, run *model.PipelineRun) error {
			return st.Runs.UpdateProgress(ctx, run.ID, run.StageRuns)
		}),
	)
	appDeployer := service.NewAppDeployer(exec, cfg.Registry)
	appDeployer.CacheRef = cfg.BuildCacheRepo
	appDeployer.Deploys = st.AppDeploys
	appDeployer.Notifier = notifDeps.Dispatcher
	// Environment (PlainVars + Secrets) injection into deployed
	// workloads, and the deployed-app URL/proxy surface.
	appDeployer.EnvResolver = &service.AppEnvResolver{Environments: st.Environments, Secrets: secMgr}
	appDeployer.Proxy = svcProxy
	appDeployer.Apps = st.Apps

	// Canary deployments (OR-1). The weighted split needs a deployer that
	// can rebalance replicas — only the Kubernetes-backed deployers
	// (kubectl/clientgo) advertise WeightedDeployer. When the configured
	// deployer can't (Noop in dev), canaryWeighted stays nil and the
	// service surfaces 422 for any canary attempt.
	var canaryWeighted deployer.WeightedDeployer
	if wd, ok := deploy.(deployer.WeightedDeployer); ok {
		canaryWeighted = wd
	}
	canarySvc := service.NewCanaryService(st.Apps, st.AppCanaries, st.AppDeploys, appDeployer, canaryWeighted,
		service.WithCanaryNotifier(notifDeps.Dispatcher))

	runs := NewRunCoordinator(st)

	const idempotencyMaxBytes = 32 << 20
	idem := idempotency.NewMemoryBounded(5*time.Minute, idempotencyMaxBytes)
	cleanups = append(cleanups, func() { idem.Close() })

	h := handler.New(st, codec, secMgr)
	h.SecretsBackend = cfg.SecretsBackend
	h.AppDeployer = appDeployer
	h.Canary = canarySvc
	// Persistent promotion/approval flow (HS26-05-01 / -08 / -14):
	// promote/approve/env-status route through this service, which
	// persists promotions + approvals and enforces RequiredApprovers.
	h.Promotions = service.NewPromotionService(st.Promotions, st.Environments)
	// Stage approval gates (HS26-05-03): the stage approve/reject handlers
	// share the same service instance the executor uses to open + poll the
	// gate, so a decision recorded over HTTP unblocks the running stage.
	h.StageApprovals = stageApprovals
	// API tokens (product-plan Tier 1): the create/list/delete handlers
	// route the role-cap / ownership / no-self-replication rules through
	// this service. The same store backs the auth middleware's `ck_`
	// token path, wired below. MFAACRValues lets the delete handler
	// step-up-gate an admin deleting another user's token.
	h.APITokens = service.NewAPITokenService(st.APITokens)
	h.MFAACRValues = cfg.OIDC.MFAACRValues
	// Enable the `ck_` bearer-token auth path. Wired unconditionally: the
	// api_tokens table backs it regardless of which interactive auth
	// method (OIDC / local / dev) is configured, so scripts and CI can
	// authenticate even when no IdP is present.
	oidcMW.EnableAPITokens(auth.NewAPITokenAuthenticator(st.APITokens))
	// Self-hosted licensing (M2 — docs/launch/01-billing-monetization.md
	// §4). The license service verifies pasted tokens against the vendor's
	// Ed25519 public key (COOKER_LICENSE_PUBLIC_KEY) and resolves the
	// current entitlements; with no public key configured it refuses
	// installs but still serves Free entitlements. Config.Validate() has
	// already rejected the "key set, public key empty" combination, so a
	// non-empty COOKER_LICENSE_KEY here implies a public key is present.
	// Decode each configured public key (COOKER_LICENSE_PUBLIC_KEYS plus the
	// back-compat singular COOKER_LICENSE_PUBLIC_KEY, already unioned in
	// config) into the verifier set. Multiple keys support rotation: a token
	// signed by any of them verifies. Invalid keys are logged and skipped
	// rather than failing boot — licensing must never take down the instance.
	var licensePubKeys []ed25519.PublicKey
	for _, raw := range cfg.License.PublicKeys {
		pk, err := license.DecodePublicKey(raw)
		if err != nil {
			slog.Error("invalid license public key; skipping it", "error", err)
			continue
		}
		licensePubKeys = append(licensePubKeys, pk)
	}
	if len(licensePubKeys) == 0 && len(cfg.License.PublicKeys) > 0 {
		slog.Error("no valid license public keys configured; licensing disabled (Free tier)")
	}
	h.License = service.NewLicenseService(st.Licenses, licensePubKeys)
	// Boot-time license install: an operator may set a signed token via
	// COOKER_LICENSE_KEY. COOKER_LICENSE_KEY is declarative — env wins when
	// the token changes — but the install must be IDEMPOTENT on token
	// identity (M2-01): re-installing the same token on every boot would
	// churn the row (fresh UUID + InstalledAt) and clobber a license an
	// admin installed via the UI (overwriting installed_by with
	// system:boot). So before installing, read the active license; if one
	// already exists whose RawToken equals the configured key, skip. Only
	// install when there is no active license OR the stored token differs.
	// On ANY failure log and degrade to Free — never panic (an
	// expired/invalid env-set license must not block boot).
	//
	// W1-09 (doc only): this boot-install is RACY under multi-replica. The
	// read-then-install is not atomic and the store keeps a single 'active'
	// license (last-write-wins via the active upsert), so N replicas booting
	// the same COOKER_LICENSE_KEY may each install once. The result converges
	// to the same token (idempotent on token identity), so the race is
	// benign in practice — but if strict single-install semantics are ever
	// required, gate this to a leader (or move it out of the per-replica boot
	// path).
	if envKey := strings.TrimSpace(cfg.License.Key); envKey != "" {
		// Read the active license first. A load error here must not block
		// boot: log and fall through to the install attempt, which carries
		// its own degrade-to-Free-on-error handling below.
		current, _, curErr := h.License.Current(ctx)
		switch {
		case curErr != nil:
			slog.Warn("could not read active license before boot install; attempting install", "error", curErr)
		case current != nil && current.RawToken == envKey:
			// Identical token already installed — do nothing. This is the
			// steady-state path on every boot after the first.
			slog.Info("license already installed from COOKER_LICENSE_KEY; skipping boot install",
				"plan", current.Plan, "customer", current.Customer, "expiresAt", current.ExpiresAt)
		}
		// Install only when there is no active license, the stored token
		// differs from the env token, or the pre-read failed.
		if curErr != nil || current == nil || current.RawToken != envKey {
			if lic, err := h.License.Install(ctx, envKey, "system:boot", ""); err != nil {
				slog.Warn("COOKER_LICENSE_KEY could not be installed; running on Free tier", "error", err)
			} else {
				slog.Info("self-hosted license installed from COOKER_LICENSE_KEY",
					"plan", lic.Plan, "customer", lic.Customer, "expiresAt", lic.ExpiresAt)
			}
		}
	}
	// AI triage (M4): Validate() already guaranteed the key when
	// enabled; nil keeps the route 503 and the frontend button hidden.
	if cfg.Triage.Enabled {
		h.Triage = triage.New(cfg.Triage.APIKey, cfg.Triage.Model)
		slog.Info("ai triage enabled", "model", h.Triage.(*triage.Client).Model)
	}
	// In-app feedback → GitHub issue relay. Token empty = feature off;
	// nil keeps the route 503 and the frontend button hidden.
	if cfg.Feedback.Token != "" {
		h.Feedback = service.NewFeedbackService(cfg.Feedback.Repo, cfg.Feedback.Token)
		slog.Info("feedback enabled", "repo", cfg.Feedback.Repo)
	} else {
		slog.Info("feedback disabled (set COOKER_FEEDBACK_GITHUB_TOKEN to enable)")
	}
	h.AppDetector = service.NewAppDetector()
	h.WSBroadcast = wsHub.Broadcast
	h.Executor = exec
	h.Runs = runs
	// Runtime panel: inspect/tail the live container or pod backing a
	// deployed compose service. CLI-backed (docker/kubectl); harmless
	// when those binaries are absent (returns "not found").
	h.Runtime = service.NewRuntimeService(cfg.Kubernetes.Namespace)
	// Kube backs the read-only Kubernetes list/inspect endpoints. Built
	// from the SAME kubeconfig source as the ClientGo deployer
	// (cfg.Kubernetes.Kubeconfig; empty => in-cluster) so reads target
	// exactly the cluster the pipeline deploys to. Lazy: a server with no
	// cluster configured still boots and the read endpoints return 503.
	h.Kube = kube.New(cfg.Kubernetes.Kubeconfig)
	// Read-only cloud inventory & cost panel (OR-2). Constructs a
	// provider per enabled cloud; with none enabled the service reports
	// Enabled()=false and the endpoints return an empty payload. Boot
	// fails if an enabled provider's SDK client can't be built (fail-fast,
	// like the secrets/deploy adapters).
	cloudInv, err := newCloudInventory(ctx, cfg.CloudInventory)
	if err != nil {
		cleanup()
		return nil, err
	}
	h.CloudInventory = cloudInv
	// HostService coordinates the PEM-bytes-to-secrets-manager
	// translation for SSH hosts. nil-safe handler-side: when secMgr
	// is nil (dev without a secrets backend), SSH host create/update
	// with a key body returns 503 — non-SSH hosts continue to work.
	h.Hosts = service.NewHostService(st, secMgr)

	// Settings registry/cluster-config services (HS26-05-04). Same
	// nil-safe posture as HostService: a Create carrying a credential
	// (registry password / cluster kubeconfig) returns 503 when secMgr
	// is nil; credential-free configs persist regardless.
	h.Registries = service.NewRegistryConfigService(st, secMgr)
	h.Clusters = service.NewClusterConfigService(st, secMgr)

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

	jobDeps, err := bootJobQueue(ctx, cfg, st, exec, notifDeps.Dispatcher)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("jobqueue boot: %w", err)
	}
	cleanups = append(cleanups, jobDeps.closeAll)
	if jobDeps.Enqueuer != nil {
		h.Enqueuer = jobDeps.Enqueuer
	}
	// Notification targets are always available (memory or Postgres),
	// independent of the job queue.
	h.NotificationTargets = notifDeps.TargetStore
	h.Dispatcher = notifDeps.Dispatcher

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

	// Canary auto-promote sweep (OR-1): a background loop that promotes
	// healthy canaries past their window and rolls back unhealthy ones.
	// Only started when a weighted deployer is configured — otherwise no
	// canary can exist to sweep. Same cancel+drain shape as the health
	// checker above.
	if canaryWeighted != nil {
		canaryCtx, canaryCancel := context.WithCancel(context.Background())
		canaryDone := make(chan struct{})
		go func() {
			defer close(canaryDone)
			_ = canarySvc.RunSweeper(canaryCtx)
		}()
		cleanups = append(cleanups, func() {
			canaryCancel()
			select {
			case <-canaryDone:
			case <-time.After(2 * time.Second):
			}
		})
	}

	// Daily retention sweep for the db audit sink: deletes rows older
	// than COOKER_AUDIT_DB_RETENTION (0 disables). Boot sweep first so
	// a server that was stopped past its window trims immediately.
	var auditSweepCancel context.CancelFunc
	var auditSweepDone chan struct{}
	if cfg.Audit.Enabled && auditHasDB(cfg.Audit.Destination) &&
		cfg.Audit.DBRetention > 0 && st.AuditEvents != nil {
		auditSweepCtx, cancel := context.WithCancel(context.Background())
		auditSweepCancel = cancel
		auditSweepDone = make(chan struct{})
		retention := cfg.Audit.DBRetention
		events := st.AuditEvents
		go func() {
			defer close(auditSweepDone)
			sweep := func() {
				c, cancelSweep := context.WithTimeout(auditSweepCtx, time.Minute)
				n, err := events.DeleteOlderThan(c, time.Now().Add(-retention))
				cancelSweep()
				if err != nil {
					slog.Warn("audit: retention sweep failed", "err", err)
				} else if n > 0 {
					slog.Info("audit: retention sweep deleted rows",
						"deleted", n, "retention", retention.String())
				}
			}
			sweep()
			t := time.NewTicker(24 * time.Hour)
			defer t.Stop()
			for {
				select {
				case <-auditSweepCtx.Done():
					return
				case <-t.C:
					sweep()
				}
			}
		}()
		cleanups = append(cleanups, func() {
			auditSweepCancel()
			select {
			case <-auditSweepDone:
			case <-time.After(2 * time.Second):
			}
		})
	}

	s := &Server{
		router:           router,
		config:           cfg,
		wsHub:            wsHub,
		oidcMW:           oidcMW,
		handler:          h,
		localAuth:        localAuthHandler,
		store:            st,
		wsTickets:        wsTickets,
		audit:            auditSink,
		traceShutdown:    traceShutdown,
		redisClient:      redisClient,
		runs:             runs,
		idempotency:      idem,
		jobQueue:         jobDeps,
		scheduler:        schedDeps,
		governance:       govClient,
		templatesDB:      templatesDBCloser,
		healthCancel:     healthCancel,
		healthDone:       healthDone,
		auditSweepCancel: auditSweepCancel,
		auditSweepDone:   auditSweepDone,
	}

	router.Use(securityHeadersMiddleware())
	router.Use(corsMiddleware(cfg.AllowedOrigins))
	if cfg.Observability.MetricsEnabled {
		// MetricsMiddleware always runs on the main router — it records
		// app traffic regardless of where /metrics is exposed.
		router.Use(observability.MetricsMiddleware())
		registerMetricsRoute(router, cfg.Observability.MetricsPort)
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

	// Dedicated metrics listener (M0-1): when a metrics port is set, serve
	// /metrics on its own http.ServeMux so the scrape endpoint is NOT
	// reachable via the public app ingress. The main router omits /metrics
	// in this mode (see New). When MetricsPort == 0, this block is skipped
	// and behaviour is unchanged (single-port; /metrics on the app router).
	var metricsSrv *http.Server
	if s.config != nil && s.config.Observability.MetricsEnabled && s.config.Observability.MetricsPort > 0 {
		mux := http.NewServeMux()
		mux.Handle("/metrics", observability.MetricsHTTPHandler())
		// Bind host for the dedicated metrics listener (W1-02). Default "" =
		// all interfaces (back-compat; also required so an in-cluster
		// ServiceMonitor can scrape the pod IP). Operators relying on this
		// dedicated port MUST protect it via NetworkPolicy or bind it to a
		// private interface (COOKER_METRICS_HOST) — it is not behind the app's
		// auth middleware. The chart side is handled separately.
		metricsHost := s.config.Observability.MetricsHost
		metricsSrv = &http.Server{
			Addr:    fmt.Sprintf("%s:%d", metricsHost, s.config.Observability.MetricsPort),
			Handler: mux,
		}
		go func() {
			// A failure here (e.g. port already bound) must not take down
			// the primary app server — but it must not be swallowed either.
			// Log it; the absence of /metrics will surface in monitoring.
			if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server failed", "addr", metricsSrv.Addr, "error", err)
			}
		}()
		slog.Info("metrics server listening on dedicated port", "addr", metricsSrv.Addr)
	}

	select {
	case err := <-errCh:
		// The primary app server exited (typically a bind/serve failure).
		// Tear down the dedicated metrics listener too so it does not leak
		// for the lifetime of the process (W1-01). The ctx.Done() path below
		// drains it gracefully; here the app is already gone, so a hard Close
		// is correct.
		if metricsSrv != nil {
			_ = metricsSrv.Close()
		}
		return err
	case <-ctx.Done():
		slog.Info("shutdown: draining HTTP server", "timeout", shutdownTimeout.String())
		drainCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if metricsSrv != nil {
			if err := metricsSrv.Shutdown(drainCtx); err != nil {
				slog.Warn("shutdown: metrics server drain failed", "error", err)
			}
		}
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
		if s.auditSweepCancel != nil {
			s.auditSweepCancel()
		}
		if s.auditSweepDone != nil {
			t := time.NewTimer(5 * time.Second)
			defer t.Stop()
			select {
			case <-s.auditSweepDone:
			case <-t.C:
				slog.Warn("shutdown: audit retention sweeper drain timed out")
			}
		}
		return <-errCh
	}
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	// BC-L2: cancel background goroutines that RunContext also cancels on
	// graceful shutdown but that Close() previously missed — so that callers
	// who invoke Close() directly (tests, deferred cleanup) don't leak them.
	if s.healthCancel != nil {
		s.healthCancel()
	}
	if s.auditSweepCancel != nil {
		s.auditSweepCancel()
	}
	// BC-H1: stop the rate-limiter gc goroutine.
	if s.rateLimiter != nil {
		s.rateLimiter.Close()
	}
	if s.webhookRateLimiter != nil {
		s.webhookRateLimiter.Close()
	}
	if s.localAuthRateLimiter != nil {
		s.localAuthRateLimiter.Close()
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
