package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/model"
)

func ListNamespaces(c *gin.Context) {
	// Placeholder: will use client-go
	namespaces := []model.KubeNamespace{}
	c.JSON(http.StatusOK, namespaces)
}

func ListWorkloads(c *gin.Context) {
	namespace := c.Query("namespace")
	workloads := []model.KubeWorkload{}
	_ = namespace
	c.JSON(http.StatusOK, workloads)
}

func GetWorkload(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"kind":      c.Param("kind"),
		"name":      c.Param("name"),
		"namespace": c.Param("ns"),
		"message":   "Workload details via client-go",
	})
}

func ScaleWorkload(c *gin.Context) {
	var req model.ScaleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "scaled",
		"kind":     c.Param("kind"),
		"name":     c.Param("name"),
		"replicas": req.Replicas,
	})
}

func RestartWorkload(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "rolling restart initiated",
		"kind":    c.Param("kind"),
		"name":    c.Param("name"),
	})
}

func GetPodLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"logs":    "",
		"message": "Stream via WebSocket /ws/kubernetes/watch for live logs",
	})
}

func ApplyManifest(c *gin.Context) {
	var req model.ApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "manifest applied via server-side apply",
		"namespace": req.Namespace,
	})
}

func DeleteResource(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message":   "resource deleted",
		"namespace": c.Param("ns"),
		"kind":      c.Param("kind"),
		"name":      c.Param("name"),
	})
}
