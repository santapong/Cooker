package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cooker-ci/cooker/internal/model"
)

// In-memory store for environments. Will be replaced with PostgreSQL.
var environments = make(map[string]*model.Environment)

func ListEnvironments(c *gin.Context) {
	result := make([]*model.Environment, 0, len(environments))
	for _, e := range environments {
		result = append(result, e)
	}
	c.JSON(http.StatusOK, result)
}

func CreateEnvironment(c *gin.Context) {
	var env model.Environment
	if err := c.ShouldBindJSON(&env); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	env.ID = uuid.New().String()
	env.CreatedAt = time.Now()
	if env.Variables == nil {
		env.Variables = make(map[string]string)
	}

	environments[env.ID] = &env
	c.JSON(http.StatusCreated, env)
}

func UpdateEnvironment(c *gin.Context) {
	id := c.Param("id")
	existing, ok := environments[id]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "environment not found"})
		return
	}

	var env model.Environment
	if err := c.ShouldBindJSON(&env); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	env.ID = id
	env.CreatedAt = existing.CreatedAt
	environments[id] = &env

	c.JSON(http.StatusOK, env)
}

func DeleteEnvironment(c *gin.Context) {
	id := c.Param("id")
	if _, ok := environments[id]; !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "environment not found"})
		return
	}
	delete(environments, id)
	c.JSON(http.StatusOK, gin.H{"message": "environment deleted"})
}

func PromoteRun(c *gin.Context) {
	runID := c.Param("runId")
	c.JSON(http.StatusOK, gin.H{
		"message": "promotion initiated",
		"runId":   runID,
	})
}

func ApprovePromotion(c *gin.Context) {
	runID := c.Param("runId")
	var req struct {
		ApprovedBy string `json:"approvedBy" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "promotion approved",
		"runId":      runID,
		"approvedBy": req.ApprovedBy,
	})
}

func GetEnvStatus(c *gin.Context) {
	runID := c.Param("runId")
	c.JSON(http.StatusOK, gin.H{
		"runId":    runID,
		"statuses": []model.EnvironmentStatus{},
	})
}
