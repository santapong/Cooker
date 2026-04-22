package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/model"
)

// Network handlers are placeholders: the real implementation talks
// to the Docker Engine API through the transport chosen by the
// host's Reachability (see internal/transport/tsnet). Until that
// lands they return empty lists / accepted stubs so the UI can
// exercise the routes.

func (h *Handler) ListDockerNetworks(c *gin.Context) {
	_ = c.Query("hostId") // will filter once the Docker client is wired
	c.JSON(http.StatusOK, []model.DockerNetwork{})
}

func (h *Handler) CreateDockerNetwork(c *gin.Context) {
	var n model.DockerNetwork
	if err := c.ShouldBindJSON(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// TODO: call (docker client).NetworkCreate(ctx, n.Name, types.NetworkCreate{...})
	c.JSON(http.StatusAccepted, gin.H{
		"status":  "pending",
		"network": n,
	})
}

func (h *Handler) GetDockerNetwork(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"id":      c.Param("id"),
		"hostId":  c.Query("hostId"),
		"message": "docker network inspect via configured host",
	})
}

func (h *Handler) DeleteDockerNetwork(c *gin.Context) {
	// TODO: call (docker client).NetworkRemove(ctx, id)
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "status": "pending"})
}

func (h *Handler) ConnectContainerToNetwork(c *gin.Context) {
	var req struct {
		ContainerID string `json:"containerId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// TODO: call (docker client).NetworkConnect(ctx, networkID, containerID, nil)
	c.JSON(http.StatusAccepted, gin.H{
		"network":   c.Param("id"),
		"container": req.ContainerID,
		"status":    "pending",
	})
}
