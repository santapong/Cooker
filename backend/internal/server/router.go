package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/handler"
)

// registerRoutes sets up all API routes.
//
// Authorization model: the /api/v1 group requires a valid OIDC
// session (or the dev user when OIDC is disabled). On top of that,
// state-changing routes are gated by auth.RequireRole so that a
// user with only RoleViewer cannot mutate resources. The two
// shortcut middlewares below — `writeRole` and `adminRole` — capture
// the two policies: most writes need operator OR admin; destructive
// deletes and credential rotations need admin. Secret reveal, webhook
// rotation, and promotion approval keep their per-handler checks so
// RBAC is enforced both at the router and at the point of sensitive
// action, which is what a reader auditing the handler expects to see.
//
// Phase-1 / A3: three sensitive routes additionally adopt the new
// auth.RequirePermission middleware as a declarative resource-action
// guard alongside the role check. Belt-and-braces — dropping one
// gate by accident still leaves the other in place. Incremental
// adoption: more routes can switch to RequirePermission in follow-ups
// without a flag day.
func (s *Server) registerRoutes() {
	// Local-auth signup + signin live OUTSIDE the /api/v1 auth group:
	// they're how an unauthenticated client gets a token in the first
	// place. /me is inside the group because it requires a valid session.
	if s.localAuth != nil {
		s.router.POST("/api/v1/auth/local/signup", s.localAuth.Signup)
		s.router.POST("/api/v1/auth/local/signin", s.localAuth.Signin)
	}
	// Public capability probe so the frontend knows which auth methods
	// to render. No auth required — every unauthenticated client needs
	// to know whether to show the SSO button, the local-auth form, or
	// both.
	s.router.GET("/api/v1/auth/methods", s.authMethods)

	api := s.router.Group("/api/v1", s.oidcMW.Handler())
	// Audit middleware sits immediately after auth so claims are in
	// context. It self-filters to mutating verbs; GETs pass through
	// untouched.
	api.Use(auditMiddleware(s.audit))

	h := s.handler
	writeRole := auth.RequireRole(auth.RoleOperator, auth.RoleAdmin)
	adminRole := auth.RequireRole(auth.RoleAdmin)
	// Step-up MFA gate. Empty COOKER_OIDC_MFA_ACR_VALUES yields a
	// passthrough; configured values require the token's `acr` (or
	// `amr`) to match. Applied to admin-only destructive operations.
	mfa := auth.RequireMFA(s.config.OIDC.MFAACRValues...)

	// Per-user rate limit applied to expensive endpoints below.
	// Disabled when COOKER_RATE_LIMIT_ENABLED=false (multi-replica
	// deployments rely on edge limiting; see SECURITY.md).
	var expensive gin.HandlerFunc
	switch {
	case !s.config.RateLimit.Enabled:
		expensive = func(c *gin.Context) { c.Next() }
	case s.config.RateLimit.Backend == "redis" && s.redisClient != nil:
		expensive = newRedisRateLimiter(s.redisClient, s.config.RateLimit.PerMinute, s.config.RateLimit.Burst).middleware()
	default:
		expensive = newRateLimiter(s.config.RateLimit.PerMinute, s.config.RateLimit.Burst).middleware()
	}

	// Pipeline routes
	pipelines := api.Group("/pipelines")
	{
		pipelines.GET("", h.ListPipelines)
		pipelines.POST("", writeRole, h.CreatePipeline)
		pipelines.GET("/:id", h.GetPipeline)
		pipelines.PUT("/:id", writeRole, h.UpdatePipeline)
		pipelines.DELETE("/:id", adminRole, mfa, h.DeletePipeline)
		pipelines.POST("/:id/validate", h.ValidatePipeline)
		// Phase-1 / A3: the run-trigger route now also passes through the
		// declarative resource-action guard. writeRole remains — belt-
		// and-braces — so an accidental policy change in one place still
		// leaves the other enforced.
		pipelines.POST("/:id/run",
			writeRole,
			auth.RequirePermission(auth.ResourcePipeline, auth.ActionInvoke),
			expensive, idempotencyMiddleware(s.idempotency), h.RunPipeline)
		pipelines.GET("/:id/runs", h.ListPipelineRuns)
		pipelines.GET("/:id/runs/:runId", h.GetPipelineRun)
		pipelines.POST("/:id/runs/:runId/cancel", writeRole, h.CancelPipelineRun)
		pipelines.GET("/:id/runs/:runId/logs/:stageId", h.GetStageLogs)
	}

	// Docker routes
	docker := api.Group("/docker")
	{
		docker.GET("/images", handler.ListDockerImages)
		docker.GET("/images/:id", handler.GetDockerImage)
		docker.POST("/images/build", writeRole, expensive, handler.BuildDockerImage)
		docker.DELETE("/images/:id", adminRole, handler.DeleteDockerImage)
		docker.GET("/containers", handler.ListContainers)
		docker.POST("/containers", writeRole, handler.CreateContainer)
		docker.POST("/containers/:id/stop", writeRole, handler.StopContainer)
		docker.DELETE("/containers/:id", adminRole, handler.DeleteContainer)
		docker.GET("/containers/:id/logs", handler.GetContainerLogs)
		docker.POST("/compose/parse", handler.ParseComposeFile)
		docker.PUT("/compose/services/:name", writeRole, handler.UpdateComposeService)

		// Networks
		docker.GET("/networks", h.ListDockerNetworks)
		docker.POST("/networks", writeRole, h.CreateDockerNetwork)
		docker.GET("/networks/:id", h.GetDockerNetwork)
		docker.DELETE("/networks/:id", adminRole, h.DeleteDockerNetwork)
		docker.POST("/networks/:id/connect", writeRole, h.ConnectContainerToNetwork)

		// Volumes
		docker.GET("/volumes", h.ListDockerVolumes)
		docker.POST("/volumes", writeRole, h.CreateDockerVolume)
		docker.GET("/volumes/:name", h.GetDockerVolume)
		docker.DELETE("/volumes/:name", adminRole, h.DeleteDockerVolume)
	}

	// OCI Registry routes
	registry := api.Group("/registry")
	{
		registry.GET("/repositories", handler.ListRepositories)
		registry.GET("/:name/tags", handler.ListTags)
		registry.GET("/:name/manifests/:ref", handler.GetManifest)
		registry.POST("/push", writeRole, handler.PushImage)
		registry.POST("/pull", writeRole, handler.PullImage)
		registry.GET("/:name/referrers/:digest", handler.GetReferrers)
	}

	// Kubernetes routes
	kubernetes := api.Group("/kubernetes")
	{
		kubernetes.GET("/namespaces", handler.ListNamespaces)
		kubernetes.GET("/workloads", handler.ListWorkloads)
		kubernetes.GET("/workloads/:ns/:kind/:name", handler.GetWorkload)
		kubernetes.POST("/workloads/:ns/:kind/:name/scale", writeRole, handler.ScaleWorkload)
		kubernetes.POST("/workloads/:ns/:kind/:name/restart", writeRole, handler.RestartWorkload)
		kubernetes.GET("/pods/:ns/:name/logs", handler.GetPodLogs)
		kubernetes.POST("/apply", writeRole, handler.ApplyManifest)
		kubernetes.DELETE("/:ns/:kind/:name", adminRole, handler.DeleteResource)
	}

	// Environment routes
	environments := api.Group("/environments")
	{
		environments.GET("", h.ListEnvironments)
		environments.POST("", writeRole, h.CreateEnvironment)
		environments.PUT("/:id", writeRole, h.UpdateEnvironment)
		environments.DELETE("/:id", adminRole, mfa, h.DeleteEnvironment)
		// Per-environment secret management. Each handler additionally
		// enforces admin-only via CanRevealSecret; the adminRole
		// middleware here is belt-and-braces so unauthenticated probing
		// of the route table gets a uniform 403 before touching the
		// handler body.
		//
		// Phase-1 / A3: secret reveal also passes through
		// RequirePermission(secret, reveal). Three layers now block a
		// non-admin from reaching the handler body — role gate, MFA gate,
		// declarative permission — plus the existing in-handler
		// CanRevealSecret check. Defense in depth on the most sensitive
		// read in the API.
		environments.GET("/:id/secrets/:key",
			adminRole, mfa,
			auth.RequirePermission(auth.ResourceSecret, auth.ActionReveal),
			h.RevealSecret)
		environments.PUT("/:id/secrets/:key", adminRole, mfa, h.PutSecret)
		environments.DELETE("/:id/secrets/:key", adminRole, mfa, h.DeleteSecret)
		// Promote secrets to another environment in a single backend
		// round-trip. Returns 501 when the configured secrets backend
		// does not implement secrets.Promoter (today: database).
		environments.POST("/:id/secrets/promote", adminRole, mfa, h.PromoteSecrets)
	}

	// Promotion routes (nested under pipeline runs)
	api.POST("/pipelines/:id/runs/:runId/promote", writeRole, h.PromoteRun)
	// ApprovePromotion keeps its CanApprovePromotion check; the handler
	// is the authority on which roles count (admin OR approver).
	api.POST("/pipelines/:id/runs/:runId/approve", h.ApprovePromotion)
	api.GET("/pipelines/:id/runs/:runId/env-status", h.GetEnvStatus)

	// App routes — the user-facing "Deploy" button and its CRUD.
	apps := api.Group("/apps")
	{
		apps.GET("", h.ListApps)
		apps.POST("", writeRole, h.CreateApp)
		apps.GET("/:id", h.GetApp)
		apps.PUT("/:id", writeRole, h.UpdateApp)
		apps.DELETE("/:id", adminRole, mfa, h.DeleteApp)
		apps.POST("/:id/deploy", writeRole, expensive, idempotencyMiddleware(s.idempotency), h.DeployApp)
		// Webhook rotation re-checks admin in the handler; keeping the
		// gate here means a viewer can't even reach it to probe codec
		// behaviour.
		//
		// Phase-1 / A3: also gated by RequirePermission(webhook, update).
		apps.PUT("/:id/webhook",
			adminRole, mfa,
			auth.RequirePermission(auth.ResourceWebhook, auth.ActionUpdate),
			h.SetAppWebhookSecret)
	}

	// Managed hosts (Phase 4).
	hosts := api.Group("/hosts")
	{
		hosts.GET("", h.ListHosts)
		hosts.POST("", writeRole, h.CreateHost)
		hosts.GET("/:id", h.GetHost)
		hosts.PUT("/:id", writeRole, h.UpdateHost)
		hosts.DELETE("/:id", adminRole, mfa, h.DeleteHost)
	}

	// GitHub webhook receiver (unauthenticated — HMAC is the auth).
	// X-GitHub-Delivery is captured by the idempotency middleware so a
	// retry from GitHub replays the original response instead of
	// triggering a duplicate deploy.
	s.router.POST("/webhooks/github", idempotencyMiddleware(s.idempotency), h.GitHubWebhook)

	// Local-auth /me lives inside the auth group because it needs a
	// session. It works for both local and OIDC sessions because the
	// underlying middleware populates the same auth.Claims either way.
	if s.localAuth != nil {
		api.GET("/auth/local/me", s.localAuth.Me)
	}

	// Settings routes — treating as admin-only because they change
	// how Cooker talks to external systems (registries, clusters).
	settings := api.Group("/settings")
	{
		settings.GET("/registries", handler.ListRegistryConfigs)
		settings.POST("/registries", adminRole, handler.AddRegistryConfig)
		settings.DELETE("/registries/:id", adminRole, handler.DeleteRegistryConfig)
		settings.GET("/clusters", handler.ListClusterConfigs)
		settings.POST("/clusters", adminRole, handler.AddClusterConfig)
	}

	// Ticket exchange for WebSocket upgrades. The browser POSTs here
	// over the authenticated /api/v1 group, gets back a single-use
	// 60-second ticket, then opens /ws/... with ?ticket=<value>.
	api.POST("/ws-tickets", func(c *gin.Context) {
		claims := auth.GetUser(c)
		if claims == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		tok, exp, err := s.wsTickets.Issue(claims.Subject)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "ticket issuance failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"ticket":     tok,
			"expires_at": exp.UTC().Format("2006-01-02T15:04:05Z"),
		})
	})

	// WebSocket routes. Each request must present a single-use
	// ticket obtained from POST /api/v1/ws-tickets. The ticket
	// gate runs before the upgrade so unauthenticated probing
	// gets 401 without ever touching the hub.
	ws := s.router.Group("/ws", s.wsTicketGate())
	{
		ws.GET("/pipeline-run/:runId", func(c *gin.Context) {
			s.wsHub.HandlePipelineRun(c.Writer, c.Request, c.Param("runId"))
		})
		ws.GET("/app-run/:runId", func(c *gin.Context) {
			s.wsHub.handleConnection(c.Writer, c.Request, "app-run:"+c.Param("runId"))
		})
		ws.GET("/docker/build/:buildId", func(c *gin.Context) {
			s.wsHub.HandleDockerBuild(c.Writer, c.Request, c.Param("buildId"))
		})
		ws.GET("/runs/:runId/stages/:stageId/logs", func(c *gin.Context) {
			s.wsHub.HandleStageLogs(c.Writer, c.Request, c.Param("runId"), c.Param("stageId"))
		})
		ws.GET("/kubernetes/watch", func(c *gin.Context) {
			s.wsHub.HandleKubeWatch(c.Writer, c.Request, c.Query("namespace"), c.Query("resource"))
		})
	}

	// Serve frontend static files in production. The OIDC redirect
	// URI (/callback) is handled entirely in the browser via PKCE —
	// the backend never sees the auth code, so no server-side
	// callback handler is needed. NoRoute serves index.html for all
	// unmatched paths, including /callback.
	s.router.NoRoute(func(c *gin.Context) {
		c.File("/usr/share/cooker/static/index.html")
	})
	s.router.Static("/assets", "/usr/share/cooker/static/assets")
}
