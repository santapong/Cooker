package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

func runtimeLogHTTPError(c *gin.Context, err error) {
	status := http.StatusServiceUnavailable
	message := service.ErrRuntimeLogUnavailable.Error()
	switch {
	case errors.Is(err, service.ErrRuntimeLogRequest):
		status = http.StatusBadRequest
		message = service.ErrRuntimeLogRequest.Error()
	case errors.Is(err, service.ErrRuntimeLogExpired):
		status = http.StatusNotFound
		message = service.ErrRuntimeLogExpired.Error()
	case errors.Is(err, service.ErrRuntimeLogCapacity):
		status = http.StatusTooManyRequests
		message = service.ErrRuntimeLogCapacity.Error()
	}
	c.JSON(status, gin.H{"error": message})
}

func (h *Handler) runtimeLogApp(c *gin.Context) (*model.App, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if h.Runtime == nil || h.Store == nil || h.Store.Apps == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "runtime inspection not configured"})
		return nil, false
	}
	app, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && app == nil) {
		c.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
		return nil, false
	}
	if err != nil {
		runtimeLogHTTPError(c, service.ErrRuntimeLogUnavailable)
		return nil, false
	}
	return app, true
}

func (h *Handler) runtimeLogResolver() *service.AppEnvResolver {
	return &service.AppEnvResolver{Environments: h.Store.Environments, Secrets: h.Secrets}
}

func (h *Handler) GetServiceLogReplicas(c *gin.Context) {
	app, ok := h.runtimeLogApp(c)
	if !ok {
		return
	}
	replicas, err := h.Runtime.DeploymentLogReplicas(c.Request.Context(), app, c.Param("svc"))
	if err != nil {
		runtimeLogHTTPError(c, err)
		return
	}
	c.JSON(http.StatusOK, replicas)
}

func (h *Handler) OpenServiceLogSession(c *gin.Context) {
	app, ok := h.runtimeLogApp(c)
	if !ok {
		return
	}
	var options service.RuntimeLogOptions
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&options)
	if err != nil && !errors.Is(err, io.EOF) {
		runtimeLogHTTPError(c, service.ErrRuntimeLogRequest)
		return
	}
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			runtimeLogHTTPError(c, service.ErrRuntimeLogRequest)
			return
		}
	}
	page, err := h.Runtime.OpenRuntimeLogSession(c.Request.Context(), app, c.Param("svc"), h.Jobs, h.runtimeLogResolver(), options)
	if err != nil {
		runtimeLogHTTPError(c, err)
		return
	}
	c.JSON(http.StatusCreated, page)
}

func (h *Handler) GetServiceLogSession(c *gin.Context) {
	app, ok := h.runtimeLogApp(c)
	if !ok {
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		runtimeLogHTTPError(c, service.ErrRuntimeLogRequest)
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if err != nil {
		runtimeLogHTTPError(c, service.ErrRuntimeLogRequest)
		return
	}
	page, err := h.Runtime.ReadRuntimeLogSession(c.Request.Context(), app.ID, c.Param("svc"), c.Param("sessionId"), h.Jobs, h.runtimeLogResolver(), offset, limit)
	if err != nil {
		runtimeLogHTTPError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) DownloadServiceLogSession(c *gin.Context) {
	app, ok := h.runtimeLogApp(c)
	if !ok {
		return
	}
	data, err := h.Runtime.DownloadRuntimeLogSession(c.Request.Context(), app.ID, c.Param("svc"), c.Param("sessionId"), h.Jobs, h.runtimeLogResolver())
	if err != nil {
		runtimeLogHTTPError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="cooker-service-logs.txt"`)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(data))
}

func (h *Handler) CloseServiceLogSession(c *gin.Context) {
	app, ok := h.runtimeLogApp(c)
	if !ok {
		return
	}
	h.Runtime.CloseRuntimeLogSession(app.ID, c.Param("svc"), c.Param("sessionId"))
	c.Status(http.StatusNoContent)
}
