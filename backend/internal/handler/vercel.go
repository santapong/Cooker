package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/provider/vercel"
)

func (h *Handler) vercelReady(c *gin.Context) bool {
	if h.Vercel == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": vercel.ErrUnavailable.Error()})
		return false
	}
	return true
}
func vercelError(c *gin.Context, err error) {
	code := http.StatusServiceUnavailable
	message := "Vercel integration is temporarily unavailable"
	switch {
	case errors.Is(err, vercel.ErrNotFound):
		code = http.StatusNotFound
		message = err.Error()
	case errors.Is(err, vercel.ErrConflict), errors.Is(err, vercel.ErrBusy):
		code = http.StatusConflict
		message = err.Error()
	case errors.Is(err, vercel.ErrInvalid), errors.Is(err, vercel.ErrDisabled):
		code = http.StatusUnprocessableEntity
		message = err.Error()
	case errors.Is(err, vercel.ErrUnavailable):
		message = err.Error()
	default:
		var apiErr *vercel.APIError
		if errors.As(err, &apiErr) {
			code = http.StatusBadGateway
			message = apiErr.Error()
			if apiErr.StatusCode == 401 || apiErr.StatusCode == 403 {
				code = http.StatusUnprocessableEntity
			}
			if apiErr.StatusCode == 429 {
				code = http.StatusTooManyRequests
				if apiErr.RetryAfter > 0 {
					c.Header("Retry-After", strconv.Itoa(int(apiErr.RetryAfter.Seconds())+1))
				}
			}
		}
	}
	c.JSON(code, gin.H{"error": message})
}
func (h *Handler) GetVercelView(c *gin.Context) {
	if !h.vercelReady(c) {
		return
	}
	view, err := h.Vercel.View(c.Request.Context())
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}
func (h *Handler) ListVercelConnections(c *gin.Context) {
	if !h.vercelReady(c) {
		return
	}
	view, err := h.Vercel.View(c.Request.Context())
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"connections": view.Connections})
}
func (h *Handler) CreateVercelConnection(c *gin.Context) {
	if !h.vercelReady(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var req vercel.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid Vercel connection"})
		return
	}
	connection, err := h.Vercel.Create(c.Request.Context(), req)
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"connection": connection})
}
func (h *Handler) UpdateVercelConnection(c *gin.Context) {
	if !h.vercelReady(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var req vercel.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid Vercel connection"})
		return
	}
	connection, err := h.Vercel.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"connection": connection})
}
func (h *Handler) DeleteVercelConnection(c *gin.Context) {
	if !h.vercelReady(c) {
		return
	}
	version, err := strconv.Atoi(c.Query("version"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connection version is required"})
		return
	}
	if err = h.Vercel.Delete(c.Request.Context(), c.Param("id"), version); err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"disconnected": true})
}
func (h *Handler) ListVercelProjects(c *gin.Context) {
	if !h.vercelReady(c) {
		return
	}
	projects, err := h.Vercel.Projects(c.Request.Context(), c.Param("id"))
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"projects": projects})
}
func (h *Handler) SyncVercelConnection(c *gin.Context) {
	if !h.vercelReady(c) {
		return
	}
	connection, err := h.Vercel.Sync(c.Request.Context(), c.Param("id"), true)
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"connection": connection})
}
