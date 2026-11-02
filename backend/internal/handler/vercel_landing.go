package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/provider/vercel"
)

func (h *Handler) publisherReady(c *gin.Context) bool {
	if h.VercelPublisher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Landing publishing requires an encrypted Vercel connection and the durable job queue"})
		return false
	}
	return true
}

func (h *Handler) GetVercelLanding(c *gin.Context) {
	if !h.publisherReady(c) {
		return
	}
	artifact, _, err := vercel.Landing()
	if err != nil {
		vercelError(c, vercel.ErrUnavailable)
		return
	}
	ops, err := h.VercelPublisher.List(c.Request.Context())
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"artifact": artifact, "operations": ops})
}

func (h *Handler) ReviewVercelLanding(c *gin.Context) {
	if !h.publisherReady(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		ConnectionID string `json:"connectionId"`
		Version      int    `json:"version"`
	}
	user := auth.GetUser(c)
	if c.ShouldBindJSON(&req) != nil || user == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Select a Vercel connection to review"})
		return
	}
	op, err := h.VercelPublisher.Review(c.Request.Context(), req.ConnectionID, req.Version, user.Subject)
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"operation": op})
}

func (h *Handler) DeployVercelLanding(c *gin.Context) {
	if !h.publisherReady(c) {
		return
	}
	user := auth.GetUser(c)
	if user == nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	op, err := h.VercelPublisher.Deploy(c.Request.Context(), c.Param("id"), user.Subject)
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"operation": op})
}

func (h *Handler) GetVercelPublication(c *gin.Context) {
	if !h.publisherReady(c) {
		return
	}
	op, err := h.VercelPublisher.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": op})
}

func (h *Handler) CheckVercelPublication(c *gin.Context) {
	if !h.publisherReady(c) {
		return
	}
	op, err := h.VercelPublisher.Check(c.Request.Context(), c.Param("id"))
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": op})
}
