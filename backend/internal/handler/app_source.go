package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/gittransport"
	"github.com/santapong/cooker/internal/validate"
)

func validateAppRepository(a *model.App) error {
	if a.BuildPlan != nil && a.BuildPlan.SourceConnectionID != "" {
		return gittransport.ValidateRepositoryPath(a.GitHubRepo, true)
	}
	return validate.GitHubRepo(a.GitHubRepo)
}

// prepareAppSource resolves credential-free identity fields server-side. An
// existing reviewed repository cannot silently switch to a replacement repo
// which happens to have acquired the same path.
func (h *Handler) prepareAppSource(c *gin.Context, a *model.App) error {
	if a.BuildPlan == nil || a.BuildPlan.SourceConnectionID == "" {
		return nil
	}
	if h.SourceConnections == nil || h.Automation == nil {
		return fmt.Errorf("source connections require the durable queue")
	}
	conn, err := h.SourceConnections.Store.Get(c.Request.Context(), a.BuildPlan.SourceConnectionID)
	if err != nil || !conn.Allows(a.GitHubRepo) {
		return fmt.Errorf("repository is not allowed by the selected source connection")
	}
	id, err := h.Automation.Source.Identity(c.Request.Context(), conn, a.GitHubRepo)
	if err != nil {
		return fmt.Errorf("cannot verify repository identity")
	}
	if a.BuildPlan.SourceRepositoryID != "" && a.BuildPlan.SourceRepositoryID != id.RepositoryID {
		return fmt.Errorf("repository identity changed; reconnect the source")
	}
	a.BuildPlan.SourceRepositoryID = id.RepositoryID
	a.BuildPlan.SourceConnectionVersion = conn.Version
	a.BuildPlan.InstallationID = conn.InstallationID
	return nil
}

func (h *Handler) AppSourceConnections(c *gin.Context) {
	if h.SourceConnections == nil {
		c.JSON(http.StatusOK, []any{})
		return
	}
	connections, err := h.SourceConnections.Store.List(c.Request.Context())
	if err != nil {
		c.JSON(503, gin.H{"error": "cannot list source connections"})
		return
	}
	items := []gin.H{}
	for _, conn := range connections {
		if conn.Enabled {
			items = append(items, gin.H{"id": conn.ID, "name": conn.Name, "provider": conn.Provider, "repositories": conn.Repositories, "version": conn.Version, "enabled": conn.Enabled})
		}
	}
	c.JSON(200, items)
}
