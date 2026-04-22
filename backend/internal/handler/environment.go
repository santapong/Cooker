package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cooker-ci/cooker/internal/model"
)

func (h *Handler) ListEnvironments(c *gin.Context) {
	envs, err := h.Store.Environments.List(c.Request.Context())
	if abortStoreErr(c, err, "environments not found") {
		return
	}
	if envs == nil {
		envs = []*model.Environment{}
	}
	c.JSON(http.StatusOK, envs)
}

func (h *Handler) CreateEnvironment(c *gin.Context) {
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

	if err := h.Store.Environments.Create(c.Request.Context(), &env); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, env)
}

func (h *Handler) UpdateEnvironment(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.Store.Environments.Get(c.Request.Context(), id)
	if abortStoreErr(c, err, "environment not found") {
		return
	}

	var env model.Environment
	if err := c.ShouldBindJSON(&env); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	env.ID = id
	env.CreatedAt = existing.CreatedAt

	if err := h.Store.Environments.Update(c.Request.Context(), &env); err != nil {
		if abortStoreErr(c, err, "environment not found") {
			return
		}
	}
	c.JSON(http.StatusOK, env)
}

func (h *Handler) DeleteEnvironment(c *gin.Context) {
	if err := h.Store.Environments.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "environment not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "environment deleted"})
}

func (h *Handler) PromoteRun(c *gin.Context) {
	runID := c.Param("runId")
	c.JSON(http.StatusOK, gin.H{
		"message": "promotion initiated",
		"runId":   runID,
	})
}

func (h *Handler) ApprovePromotion(c *gin.Context) {
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

func (h *Handler) GetEnvStatus(c *gin.Context) {
	runID := c.Param("runId")
	c.JSON(http.StatusOK, gin.H{
		"runId":    runID,
		"statuses": []model.EnvironmentStatus{},
	})
}
