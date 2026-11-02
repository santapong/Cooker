package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/validate"
)

func (h *Handler) AppDeploymentCapabilities(c *gin.Context) {
	caps := service.AppTargetCapabilities()
	for i := range caps {
		cap := &caps[i]
		app := &model.App{DeployTarget: model.DeployTarget{Kind: cap.Kind}}
		if h.AppDeployer == nil {
			cap.Available = false
			cap.Reason = "app deployer is not configured"
		} else if err := h.AppDeployer.CheckRegistryDeployment(app); err != nil {
			cap.Available = false
			cap.Reason = err.Error()
		} else if h.AppDeployer.CheckExecution != nil {
			if err := h.AppDeployer.CheckExecution(app, false); err != nil {
				caps[i].Available = false
				caps[i].Reason = err.Error()
			}
		}
		cap.SourceBuild.Reason = "source build preflight is not configured"
		if !cap.Available {
			cap.SourceBuild.Reason = cap.Reason
		}
		if h.AppDeployer != nil && h.AppDeployer.CheckExecution != nil && cap.Available {
			if err := h.AppDeployer.CheckExecution(app, true); err != nil {
				cap.SourceBuild.Reason = err.Error()
			} else {
				cap.SourceBuild = service.CapabilityState{Available: true}
			}
		}
		cap.ServiceLogs.Reason = "runtime log adapter is not configured for this target"
		if cap.Kind == model.DeployTargetKubernetes {
			cap.ServiceLogs.Reason = "Kubernetes logs require an immutable pod session adapter; service-name streaming is disabled"
		}
		if h.Runtime != nil && h.Runtime.Store != nil && h.Jobs != nil && cap.Available && cap.Kind == model.DeployTargetDockerHost {
			cap.ServiceLogs = service.CapabilityState{Available: true}
		}
	}
	c.JSON(http.StatusOK, gin.H{"targets": caps})
}

// InspectAppRepository never builds or deploys. It clones a permitted repository,
// resolves the reviewed commit, and returns a redacted execution preview.
func (h *Handler) InspectAppRepository(c *gin.Context) {
	if h.AppDetector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "repository inspection is not available"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	var app model.App
	if err := c.ShouldBindJSON(&app); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid inspection request"})
		return
	}
	if err := validateAppRepository(&app); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validate.GitRefName("branch", app.Branch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.ValidateAppDeployment(&app); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.prepareAppSource(c, &app); err != nil {
		c.JSON(422, gin.H{"error": err.Error()})
		return
	}
	// Inspection only exposes configured key names; values are masked by the
	// service before it returns. Use the same resolver as App deployment.
	env := map[string]string{}
	inspectCtx := service.WithLogSecrets(c.Request.Context(), nil)
	if app.EnvironmentID != "" {
		if h.AppDeployer == nil || h.AppDeployer.EnvResolver == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "environment resolution is not configured"})
			return
		}
		var err error
		env, err = h.AppDeployer.EnvResolver.Resolve(inspectCtx, &app)
		if err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "cannot resolve selected environment"})
			return
		}
	}
	result, err := h.AppDetector.Inspect(inspectCtx, &app, env)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if app.BuildPlan != nil {
		result.SourceConnectionID = app.BuildPlan.SourceConnectionID
		result.SourceRepositoryID = app.BuildPlan.SourceRepositoryID
		result.SourceConnectionVersion = app.BuildPlan.SourceConnectionVersion
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GitHubConnection(c *gin.Context) {
	client := h.GitHubSource
	if client == nil || !client.Configured() {
		c.JSON(http.StatusOK, gin.H{"configured": false, "installUrl": "", "installations": []any{}, "message": "Ask a Cooker administrator to configure the GitHub App connection. Public repositories can be entered manually."})
		return
	}
	installations, err := client.Installations(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"configured": true, "installUrl": client.InstallURL(), "installations": installations, "message": "GitHub App access is shared with Cooker operators for administrator-approved installations."})
}

func (h *Handler) GitHubRepositories(c *gin.Context) {
	if h.GitHubSource == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GitHub connection is not configured"})
		return
	}
	id, err := strconv.ParseInt(c.Query("installationId"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "select a GitHub installation"})
		return
	}
	page := intQuery(c, "page", 1, 1, 1000)
	items, err := h.GitHubSource.Repositories(c.Request.Context(), id, page)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"repositories": items, "hasMore": len(items) == 100})
}

func (h *Handler) GitHubRevisions(c *gin.Context) {
	if h.GitHubSource == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "repository connection is not configured"})
		return
	}
	repo := c.Query("repo")
	if err := validate.GitHubRepo(repo); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, parseErr := strconv.ParseInt(c.DefaultQuery("installationId", "0"), 10, 64)
	if parseErr != nil || id < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid installation"})
		return
	}
	items, err := h.GitHubSource.Revisions(c.Request.Context(), id, repo, c.Query("kind"), intQuery(c, "page", 1, 1, 1000))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revisions": items, "hasMore": len(items) == 100})
}

func (h *Handler) checkAppPrefix(c *gin.Context, app *model.App) error {
	if app.DeployTarget.Prefix == "" {
		return nil
	}
	apps, err := h.Store.Apps.List(c.Request.Context(), 0, 0)
	if err != nil {
		return fmt.Errorf("cannot check deployment prefix")
	}
	for _, other := range apps {
		if other.ID == app.ID {
			continue
		}
		if service.AppPrefix(other) == app.DeployTarget.Prefix && model.DeploymentScope(other.DeployTarget) == model.DeploymentScope(app.DeployTarget) {
			return fmt.Errorf("deployment prefix is already used on this target")
		}
	}
	return nil
}
