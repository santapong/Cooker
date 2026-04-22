package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/model"
)

// Volume handlers are placeholders alongside the network handlers;
// the real impl dials the Docker Engine API through the host's
// transport.

func (h *Handler) ListDockerVolumes(c *gin.Context) {
	_ = c.Query("hostId")
	c.JSON(http.StatusOK, []model.DockerVolume{})
}

func (h *Handler) CreateDockerVolume(c *gin.Context) {
	var v model.DockerVolume
	if err := c.ShouldBindJSON(&v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// TODO: call (docker client).VolumeCreate(ctx, types.VolumeCreateBody{...})
	c.JSON(http.StatusAccepted, gin.H{
		"status": "pending",
		"volume": v,
	})
}

func (h *Handler) GetDockerVolume(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name":    c.Param("name"),
		"hostId":  c.Query("hostId"),
		"message": "docker volume inspect via configured host",
	})
}

func (h *Handler) DeleteDockerVolume(c *gin.Context) {
	// TODO: call (docker client).VolumeRemove(ctx, name, false)
	c.JSON(http.StatusOK, gin.H{"name": c.Param("name"), "status": "pending"})
}
