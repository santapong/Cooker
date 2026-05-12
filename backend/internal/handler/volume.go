package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
)

// Volume handlers, like network handlers, depend on the host
// transport that is not yet plumbed through Handler. They return 501
// for write operations and an empty list for reads, with the same
// shape as network.go so the UI can detect the gap consistently.

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
	notImplementedDockerHost(c, "volume.create")
}

func (h *Handler) GetDockerVolume(c *gin.Context) {
	notImplementedDockerHost(c, "volume.inspect")
}

func (h *Handler) DeleteDockerVolume(c *gin.Context) {
	notImplementedDockerHost(c, "volume.remove")
}
