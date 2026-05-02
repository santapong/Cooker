package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/model"
)

// Network handlers route to the configured Docker host (selected via
// /api/v1/hosts and reached via the tsnet/local transport). The
// transport plumbing is not yet wired through Handler — until it is,
// these endpoints return 501 with a consistent shape so clients can
// detect the feature gap rather than silently consuming fake IDs.
//
// Tracked in backlog P6 (handler placeholders) + P9.4 (tsnet transport
// build-tag). Listing endpoints return an empty list with HTTP 200 so
// UI pages can render their empty state without surfacing an error.

func notImplementedDockerHost(c *gin.Context, op string) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":     "docker host transport not configured",
		"operation": op,
		"hint":      "configure a host via POST /api/v1/hosts and ensure COOKER_DOCKER_HOST is reachable",
	})
}

func (h *Handler) ListDockerNetworks(c *gin.Context) {
	_ = c.Query("hostId")
	c.JSON(http.StatusOK, []model.DockerNetwork{})
}

func (h *Handler) CreateDockerNetwork(c *gin.Context) {
	var n model.DockerNetwork
	if err := c.ShouldBindJSON(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	notImplementedDockerHost(c, "network.create")
}

func (h *Handler) GetDockerNetwork(c *gin.Context) {
	notImplementedDockerHost(c, "network.inspect")
}

func (h *Handler) DeleteDockerNetwork(c *gin.Context) {
	notImplementedDockerHost(c, "network.remove")
}

func (h *Handler) ConnectContainerToNetwork(c *gin.Context) {
	var req struct {
		ContainerID string `json:"containerId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	notImplementedDockerHost(c, "network.connect")
}
