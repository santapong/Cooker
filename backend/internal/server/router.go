package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/handler"
)

// registerRoutes sets up all API routes.
func (s *Server) registerRoutes() {
	// All /api/v1 routes require a valid OIDC session (or the dev user
	// when OIDC is disabled).
	api := s.router.Group("/api/v1", s.oidcMW.Handler())

	h := s.handler

	// Pipeline routes
	pipelines := api.Group("/pipelines")
	{
		pipelines.GET("", h.ListPipelines)
		pipelines.POST("", h.CreatePipeline)
		pipelines.GET("/:id", h.GetPipeline)
		pipelines.PUT("/:id", h.UpdatePipeline)
		pipelines.DELETE("/:id", h.DeletePipeline)
		pipelines.POST("/:id/validate", h.ValidatePipeline)
		pipelines.POST("/:id/run", h.RunPipeline)
		pipelines.GET("/:id/runs", h.ListPipelineRuns)
		pipelines.GET("/:id/runs/:runId", h.GetPipelineRun)
		pipelines.POST("/:id/runs/:runId/cancel", h.CancelPipelineRun)
		pipelines.GET("/:id/runs/:runId/logs/:stageId", h.GetStageLogs)
	}

	// Docker routes
	docker := api.Group("/docker")
	{
		docker.GET("/images", handler.ListDockerImages)
		docker.GET("/images/:id", handler.GetDockerImage)
		docker.POST("/images/build", handler.BuildDockerImage)
		docker.DELETE("/images/:id", handler.DeleteDockerImage)
		docker.GET("/containers", handler.ListContainers)
		docker.POST("/containers", handler.CreateContainer)
		docker.POST("/containers/:id/stop", handler.StopContainer)
		docker.DELETE("/containers/:id", handler.DeleteContainer)
		docker.GET("/containers/:id/logs", handler.GetContainerLogs)
		docker.POST("/compose/parse", handler.ParseComposeFile)
		docker.PUT("/compose/services/:name", handler.UpdateComposeService)
	}

	// OCI Registry routes
	registry := api.Group("/registry")
	{
		registry.GET("/repositories", handler.ListRepositories)
		registry.GET("/:name/tags", handler.ListTags)
		registry.GET("/:name/manifests/:ref", handler.GetManifest)
		registry.POST("/push", handler.PushImage)
		registry.POST("/pull", handler.PullImage)
		registry.GET("/:name/referrers/:digest", handler.GetReferrers)
	}

	// Kubernetes routes
	kubernetes := api.Group("/kubernetes")
	{
		kubernetes.GET("/namespaces", handler.ListNamespaces)
		kubernetes.GET("/workloads", handler.ListWorkloads)
		kubernetes.GET("/workloads/:ns/:kind/:name", handler.GetWorkload)
		kubernetes.POST("/workloads/:ns/:kind/:name/scale", handler.ScaleWorkload)
		kubernetes.POST("/workloads/:ns/:kind/:name/restart", handler.RestartWorkload)
		kubernetes.GET("/pods/:ns/:name/logs", handler.GetPodLogs)
		kubernetes.POST("/apply", handler.ApplyManifest)
		kubernetes.DELETE("/:ns/:kind/:name", handler.DeleteResource)
	}

	// Environment routes
	environments := api.Group("/environments")
	{
		environments.GET("", h.ListEnvironments)
		environments.POST("", h.CreateEnvironment)
		environments.PUT("/:id", h.UpdateEnvironment)
		environments.DELETE("/:id", h.DeleteEnvironment)
		// Per-environment secret management. Each handler enforces
		// its own RBAC — admin-only for read, write, and delete.
		environments.GET("/:id/secrets/:key", h.RevealSecret)
		environments.PUT("/:id/secrets/:key", h.PutSecret)
		environments.DELETE("/:id/secrets/:key", h.DeleteSecret)
	}

	// Promotion routes (nested under pipeline runs)
	api.POST("/pipelines/:id/runs/:runId/promote", h.PromoteRun)
	api.POST("/pipelines/:id/runs/:runId/approve", h.ApprovePromotion)
	api.GET("/pipelines/:id/runs/:runId/env-status", h.GetEnvStatus)

	// App routes — the user-facing "Deploy" button and its CRUD.
	apps := api.Group("/apps")
	{
		apps.GET("", h.ListApps)
		apps.POST("", h.CreateApp)
		apps.GET("/:id", h.GetApp)
		apps.PUT("/:id", h.UpdateApp)
		apps.DELETE("/:id", h.DeleteApp)
		apps.POST("/:id/deploy", h.DeployApp)
		apps.PUT("/:id/webhook", h.SetAppWebhookSecret)
	}

	// Managed hosts (Phase 4).
	hosts := api.Group("/hosts")
	{
		hosts.GET("", h.ListHosts)
		hosts.POST("", h.CreateHost)
		hosts.GET("/:id", h.GetHost)
		hosts.PUT("/:id", h.UpdateHost)
		hosts.DELETE("/:id", h.DeleteHost)
	}

	// GitHub webhook receiver (unauthenticated — HMAC is the auth).
	s.router.POST("/webhooks/github", h.GitHubWebhook)

	// Settings routes
	settings := api.Group("/settings")
	{
		settings.GET("/registries", handler.ListRegistryConfigs)
		settings.POST("/registries", handler.AddRegistryConfig)
		settings.DELETE("/registries/:id", handler.DeleteRegistryConfig)
		settings.GET("/clusters", handler.ListClusterConfigs)
		settings.POST("/clusters", handler.AddClusterConfig)
	}

	// WebSocket routes (unauthenticated for now; see server.go comment)
	ws := s.router.Group("/ws")
	{
		ws.GET("/pipeline-run/:runId", func(c *gin.Context) {
			s.wsHub.HandlePipelineRun(c.Writer, c.Request, c.Param("runId"))
		})
		ws.GET("/docker/build/:buildId", func(c *gin.Context) {
			s.wsHub.HandleDockerBuild(c.Writer, c.Request, c.Param("buildId"))
		})
		ws.GET("/kubernetes/watch", func(c *gin.Context) {
			s.wsHub.HandleKubeWatch(c.Writer, c.Request, c.Query("namespace"), c.Query("resource"))
		})
	}

	// Serve frontend static files in production
	s.router.NoRoute(func(c *gin.Context) {
		c.File("/usr/share/cooker/static/index.html")
	})
	s.router.Static("/assets", "/usr/share/cooker/static/assets")

	// Auth callback (for OIDC redirect)
	s.router.GET("/auth/callback", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "OIDC callback placeholder"})
	})
}
