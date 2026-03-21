package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/model"
)

func ListDockerImages(c *gin.Context) {
	// Placeholder: will use Docker Engine SDK
	images := []model.ImageInfo{}
	c.JSON(http.StatusOK, images)
}

func GetDockerImage(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"id":          c.Param("id"),
		"message":     "Image details with OCI manifest will be populated via Docker Engine SDK",
		"ociManifest": nil,
	})
}

func BuildDockerImage(c *gin.Context) {
	var req struct {
		Dockerfile string            `json:"dockerfile"`
		Context    string            `json:"context"`
		Tags       []string          `json:"tags"`
		BuildArgs  map[string]string `json:"buildArgs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Placeholder: will stream build output via WebSocket
	c.JSON(http.StatusAccepted, gin.H{
		"buildId": "build-placeholder",
		"message": "Build initiated. Connect to WS /ws/docker/build/<buildId> for logs.",
	})
}

func DeleteDockerImage(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "image deleted", "id": c.Param("id")})
}

func ListContainers(c *gin.Context) {
	containers := []model.ContainerInfo{}
	c.JSON(http.StatusOK, containers)
}

func CreateContainer(c *gin.Context) {
	var req struct {
		Image   string            `json:"image" binding:"required"`
		Name    string            `json:"name"`
		Ports   []model.PortBinding `json:"ports"`
		Env     map[string]string `json:"env"`
		Command []string          `json:"command"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":      "container-placeholder",
		"message": "Container created via Docker Engine SDK",
	})
}

func StopContainer(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "stopped", "id": c.Param("id")})
}

func DeleteContainer(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "removed", "id": c.Param("id")})
}

func GetContainerLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"logs": "", "message": "Stream via WebSocket for live logs"})
}
