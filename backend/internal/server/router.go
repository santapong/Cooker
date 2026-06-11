package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/governance"
	"github.com/santapong/cooker/internal/handler"
)

func (s *Server) registerRoutes() {
	if s.localAuth != nil {
		s.router.POST("/api/v1/auth/local/signup", s.localAuth.Signup)
		s.router.POST("/api/v1/auth/local/signin", s.localAuth.Signin)
	}
	s.router.GET("/api/v1/auth/methods", s.authMethods)

	api := s.router.Group("/api/v1", s.oidcMW.Handler())
	api.Use(auditMiddleware(s.audit))

	// Optional-feature discovery for the frontend (authenticated so
	// config posture isn't leaked publicly).
	api.GET("/capabilities", func(c *gin.Context) { s.handler.GetCapabilities(c) })

	h := s.handler
	writeRole := auth.RequireRole(auth.RoleOperator, auth.RoleAdmin)
	adminRole := auth.RequireRole(auth.RoleAdmin)
	mfa := auth.RequireMFA(s.config.OIDC.MFAACRValues...)

	var expensive gin.HandlerFunc
	switch {
	case !s.config.RateLimit.Enabled:
		expensive = func(c *gin.Context) { c.Next() }
	case s.config.RateLimit.Backend == "redis" && s.redisClient != nil:
		expensive = newRedisRateLimiter(s.redisClient, s.config.RateLimit.PerMinute, s.config.RateLimit.Burst).middleware()
	default:
		expensive = newRateLimiter(s.config.RateLimit.PerMinute, s.config.RateLimit.Burst).middleware()
	}

	pipelines := api.Group("/pipelines")
	{
		pipelines.GET("", h.ListPipelines)
		pipelines.POST("", writeRole, h.CreatePipeline)
		pipelines.GET("/:id", h.GetPipeline)
		pipelines.PUT("/:id", writeRole, h.UpdatePipeline)
		pipelines.DELETE("/:id", adminRole, mfa, h.DeletePipeline)
		pipelines.POST("/:id/validate", h.ValidatePipeline)
		pipelines.POST("/:id/run",
			writeRole,
			auth.RequirePermission(auth.ResourcePipeline, auth.ActionInvoke),
			expensive, idempotencyMiddleware(s.idempotency), h.RunPipeline)
		pipelines.GET("/:id/runs", h.ListPipelineRuns)
		pipelines.GET("/:id/runs/:runId", h.GetPipelineRun)
		pipelines.POST("/:id/runs/:runId/cancel", writeRole, h.CancelPipelineRun)
		pipelines.GET("/:id/runs/:runId/logs/:stageId", h.GetStageLogs)
		// Run diff (roadmap M3): compare a run against last-success (or
		// an explicit ?against=<runId>).
		pipelines.GET("/:id/runs/:runId/diff", h.GetRunDiff)
		// AI failure triage (roadmap M4): paid upstream call → same
		// rate limit as the other expensive routes.
		pipelines.POST("/:id/runs/:runId/stages/:stageId/triage", writeRole, expensive, h.TriageStage)
		// Stage-duration analytics from run history (roadmap M4).
		pipelines.GET("/:id/analytics", h.GetPipelineAnalytics)
		// Phase-2 / F4: create a pipeline from a catalog template. Same
		// writeRole gate as POST /pipelines (creating-via-template is
		// just a parameterised create).
		pipelines.POST("/from-template/:id", writeRole, h.CreatePipelineFromTemplate)
	}

	// Phase-2 / F4 templates catalog. Read endpoints are available to
	// any authenticated user; the gallery view is metadata only.
	templates := api.Group("/templates")
	{
		templates.GET("", h.ListTemplates)
		templates.GET("/:id", h.GetTemplate)
	}

	// Admin CRUD for the Phase-1 + Phase-2 catalogs. All endpoints are
	// admin-only and MFA-gated; nil stores return 503 (handlers handle
	// the nil check internally so a dev-mode boot still serves the
	// rest of the API).
	admin := api.Group("/admin", adminRole, mfa)
	{
		adminTemplates := admin.Group("/templates")
		{
			adminTemplates.POST("", h.CreateTemplate)
			adminTemplates.PUT("/:id", h.UpdateTemplate)
			adminTemplates.DELETE("/:id", h.DeleteTemplate)
		}
		adminSchedules := admin.Group("/schedules")
		{
			adminSchedules.GET("", h.ListSchedules)
			adminSchedules.GET("/:id", h.GetSchedule)
			adminSchedules.POST("", h.CreateSchedule)
			adminSchedules.PUT("/:id", h.UpdateSchedule)
			adminSchedules.DELETE("/:id", h.DeleteSchedule)
		}
		adminNotificationTargets := admin.Group("/notification-targets")
		{
			adminNotificationTargets.GET("", h.ListNotificationTargets)
			adminNotificationTargets.GET("/:id", h.GetNotificationTarget)
			adminNotificationTargets.POST("", h.CreateNotificationTarget)
			adminNotificationTargets.PUT("/:id", h.UpdateNotificationTarget)
			adminNotificationTargets.DELETE("/:id", h.DeleteNotificationTarget)
		}
		// Queryable audit trail (roadmap M5). Backed by the db audit
		// sink; the memory store serves a bounded ring in dev.
		admin.GET("/audit", h.ListAuditEvents)
	}

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
		docker.GET("/networks", h.ListDockerNetworks)
		docker.POST("/networks", writeRole, h.CreateDockerNetwork)
		docker.GET("/networks/:id", h.GetDockerNetwork)
		docker.DELETE("/networks/:id", adminRole, h.DeleteDockerNetwork)
		docker.POST("/networks/:id/connect", writeRole, h.ConnectContainerToNetwork)
		docker.GET("/volumes", h.ListDockerVolumes)
		docker.POST("/volumes", writeRole, h.CreateDockerVolume)
		docker.GET("/volumes/:name", h.GetDockerVolume)
		docker.DELETE("/volumes/:name", adminRole, h.DeleteDockerVolume)
	}

	registry := api.Group("/registry")
	{
		registry.GET("/repositories", handler.ListRepositories)
		registry.GET("/:name/tags", handler.ListTags)
		registry.GET("/:name/manifests/:ref", handler.GetManifest)
		registry.POST("/push", writeRole, handler.PushImage)
		registry.POST("/pull", writeRole, handler.PullImage)
		registry.GET("/:name/referrers/:digest", handler.GetReferrers)
	}

	kubernetes := api.Group("/kubernetes")
	{
		// Read-only list/inspect: real client-go reads against the
		// server's configured cluster (Handler methods, h.Kube-backed).
		// Gated at writeRole (operator+): these expose live cluster state
		// — namespaces, workloads, and especially pod logs, which can
		// contain tokens/PII across every namespace the (cluster-wide by
		// chart default) ServiceAccount can see. A plain viewer must not be
		// able to read arbitrary cluster pod logs, so cluster introspection
		// requires the same operator role as the k8s write path below.
		// See SECURITY.md "Kubernetes Access".
		kubernetes.GET("/namespaces", writeRole, h.ListNamespaces)
		kubernetes.GET("/workloads", writeRole, h.ListWorkloads)
		kubernetes.GET("/workloads/:ns/:kind/:name", writeRole, h.GetWorkload)
		kubernetes.GET("/pods/:ns/:name/logs", writeRole, h.GetPodLogs)
		// Write path: still package-level stubs (scale/restart/apply/
		// delete) — separate work, does not use h.Kube.
		kubernetes.POST("/workloads/:ns/:kind/:name/scale", writeRole, handler.ScaleWorkload)
		kubernetes.POST("/workloads/:ns/:kind/:name/restart", writeRole, handler.RestartWorkload)
		kubernetes.POST("/apply", writeRole, handler.ApplyManifest)
		kubernetes.DELETE("/:ns/:kind/:name", adminRole, handler.DeleteResource)
	}

	environments := api.Group("/environments")
	{
		environments.GET("", h.ListEnvironments)
		environments.POST("", writeRole, h.CreateEnvironment)
		environments.PUT("/:id", writeRole, h.UpdateEnvironment)
		environments.DELETE("/:id", adminRole, mfa, h.DeleteEnvironment)
		environments.GET("/:id/secrets/:key",
			adminRole, mfa,
			auth.RequirePermission(auth.ResourceSecret, auth.ActionReveal),
			h.RevealSecret)
		environments.PUT("/:id/secrets/:key", adminRole, mfa, h.PutSecret)
		environments.DELETE("/:id/secrets/:key", adminRole, mfa, h.DeleteSecret)
		environments.POST("/:id/secrets/promote", adminRole, mfa, h.PromoteSecrets)
	}

	api.POST("/pipelines/:id/runs/:runId/promote", writeRole, h.PromoteRun)
	api.POST("/pipelines/:id/runs/:runId/approve", h.ApprovePromotion)
	api.GET("/pipelines/:id/runs/:runId/env-status", h.GetEnvStatus)

	// Stage-level approval gates (StageTypeApproval, HS26-05-03). RBAC is
	// an inline admin-or-approver check in the approve/reject handlers,
	// matching the promotion /approve route above (no route-level role
	// middleware). The list endpoint is read-only for any authenticated
	// user (the run page polls it to surface awaiting gates).
	api.GET("/pipelines/:id/runs/:runId/stage-approvals", h.ListStageApprovals)
	api.POST("/pipelines/:id/runs/:runId/stages/:stageId/approve", h.ApproveStage)
	api.POST("/pipelines/:id/runs/:runId/stages/:stageId/reject", h.RejectStage)

	// Governance admission hook (Phase-4). The middleware is a no-op when
	// COOKER_GOVERNANCE_URL is empty, so this is safe to wire unconditionally.
	// Pipeline-defined deploy stages are caught by the executor pre-stage hook
	// (Milestone C — service.WithDeployGovernanceHook wired in server.go).
	// The middleware here gates the synchronous /apps/:id/deploy entrypoint.
	govDeploy := auth.RequireGovernanceAllow(
		s.governance,
		governance.AppDeployExtractor(s.store),
		auth.BreakGlassOption{Enabled: s.config.Governance.BreakGlassEnabled},
	)

	apps := api.Group("/apps")
	{
		apps.GET("", h.ListApps)
		apps.POST("", writeRole, h.CreateApp)
		// Pre-import build detection for the New-App wizard. Static
		// segment beside /:id siblings; network-bound (shallow clone)
		// → rate-limited like deploy.
		apps.POST("/detect-build", writeRole, expensive, h.DetectAppBuild)
		apps.GET("/:id", h.GetApp)
		apps.PUT("/:id", writeRole, h.UpdateApp)
		apps.DELETE("/:id", adminRole, mfa, h.DeleteApp)
		apps.POST("/:id/deploy", writeRole, expensive, idempotencyMiddleware(s.idempotency), govDeploy, h.DeployApp)
		// Deploy history + one-click rollback + drift (roadmap M3). A
		// rollback IS a deploy: same rate limit, idempotency and
		// governance gates. Drift is writeRole because it reveals live
		// cluster state (same rationale as the kubernetes read path).
		apps.GET("/:id/deploys", h.ListAppDeploys)
		apps.POST("/:id/rollback", writeRole, expensive, idempotencyMiddleware(s.idempotency), govDeploy, h.RollbackApp)
		apps.GET("/:id/drift", writeRole, h.GetAppDrift)
		apps.PUT("/:id/webhook",
			adminRole, mfa,
			auth.RequirePermission(auth.ResourceWebhook, auth.ActionUpdate),
			h.SetAppWebhookSecret)
		// Deployment-view runtime panel: live container/pod state for one
		// service of the app's compose deployment DAG.
		apps.GET("/:id/services/:svc/runtime", h.GetServiceRuntime)
	}

	hosts := api.Group("/hosts")
	{
		hosts.GET("", h.ListHosts)
		hosts.POST("", writeRole, h.CreateHost)
		hosts.GET("/:id", h.GetHost)
		hosts.PUT("/:id", writeRole, h.UpdateHost)
		hosts.DELETE("/:id", adminRole, mfa, h.DeleteHost)
	}

	// Git provider webhook receivers (unauthenticated — each provider's
	// signature / token header is the authentication).
	s.router.POST("/webhooks/github", idempotencyMiddleware(s.idempotency), h.GitHubWebhook)
	s.router.POST("/webhooks/gitlab", idempotencyMiddleware(s.idempotency), h.GitLabWebhook)
	s.router.POST("/webhooks/bitbucket", idempotencyMiddleware(s.idempotency), h.BitbucketWebhook)
	s.router.POST("/webhooks/gitea", idempotencyMiddleware(s.idempotency), h.GiteaWebhook)

	if s.localAuth != nil {
		api.GET("/auth/local/me", s.localAuth.Me)
	}

	settings := api.Group("/settings")
	{
		settings.GET("/registries", h.ListRegistryConfigs)
		settings.POST("/registries", adminRole, h.AddRegistryConfig)
		settings.DELETE("/registries/:id", adminRole, h.DeleteRegistryConfig)
		settings.GET("/clusters", h.ListClusterConfigs)
		settings.POST("/clusters", adminRole, h.AddClusterConfig)
		settings.DELETE("/clusters/:id", adminRole, h.DeleteClusterConfig)
		// Secrets-backend connectivity probe (roadmap M5 / F12). The
		// settings group carries no MFA gate, so this admin action
		// adds it explicitly, matching the /admin group's posture.
		settings.POST("/secrets/test", adminRole, mfa, h.TestSecretsBackend)
	}

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
		// Live container/pod logs for a deployed service (runtime panel).
		ws.GET("/runtime/:appId/:serviceId/logs", func(c *gin.Context) {
			appID, svc := c.Param("appId"), c.Param("serviceId")
			produce, ok := h.RuntimeLogsProducer(appID, svc)
			if !ok {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "runtime logs not available"})
				return
			}
			s.wsHub.HandleRuntimeLogs(c.Writer, c.Request, appID, svc, produce)
		})
		ws.GET("/kubernetes/watch", func(c *gin.Context) {
			s.wsHub.HandleKubeWatch(c.Writer, c.Request, c.Query("namespace"), c.Query("resource"))
		})
	}

	s.router.NoRoute(spaIndexHandler("/usr/share/cooker/static/index.html"))
	s.router.GET("/assets/*filepath", assetsHandler("/usr/share/cooker/static/assets"))
	s.router.HEAD("/assets/*filepath", assetsHandler("/usr/share/cooker/static/assets"))
}
