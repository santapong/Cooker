package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/service"
)

func nativeLogHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
}

func nativeLogSubject(c *gin.Context) service.NativeRuntimeSubject {
	return service.NativeRuntimeSubject{Kind: "native-stage", PipelineID: c.Param("id"), RunID: c.Param("runId"), StageID: c.Param("stageId")}
}

func (h *Handler) nativeLogReady(c *gin.Context) bool {
	nativeLogHeaders(c)
	if h.Runtime == nil {
		runtimeLogHTTPError(c, service.ErrRuntimeLogUnavailable)
		return false
	}
	return true
}

func (h *Handler) nativeLogResolver() *service.AppEnvResolver {
	if h.Store == nil {
		return nil
	}
	return &service.AppEnvResolver{Environments: h.Store.Environments, Secrets: h.Secrets}
}

func (h *Handler) GetNativeStageLogReplicas(c *gin.Context) {
	if !h.nativeLogReady(c) {
		return
	}
	result, err := h.Runtime.NativeStageLogReplicas(c.Request.Context(), nativeLogSubject(c), h.Jobs)
	if err != nil {
		runtimeLogHTTPError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) OpenNativeStageLogSession(c *gin.Context) {
	if !h.nativeLogReady(c) {
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
	page, err := h.Runtime.OpenNativeStageLogSession(c.Request.Context(), nativeLogSubject(c), h.Jobs, h.nativeLogResolver(), options)
	if err != nil {
		runtimeLogHTTPError(c, err)
		return
	}
	c.JSON(http.StatusCreated, page)
}

func (h *Handler) GetNativeStageLogSession(c *gin.Context) {
	if !h.nativeLogReady(c) {
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		runtimeLogHTTPError(c, service.ErrRuntimeLogRequest)
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if err != nil || limit < 1 || limit > 500 {
		runtimeLogHTTPError(c, service.ErrRuntimeLogRequest)
		return
	}
	page, err := h.Runtime.ReadNativeStageLogSession(c.Request.Context(), nativeLogSubject(c), c.Param("sessionId"), h.Jobs, h.nativeLogResolver(), offset, limit)
	if err != nil {
		runtimeLogHTTPError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) DownloadNativeStageLogSession(c *gin.Context) {
	if !h.nativeLogReady(c) {
		return
	}
	data, err := h.Runtime.DownloadNativeStageLogSession(c.Request.Context(), nativeLogSubject(c), c.Param("sessionId"), h.Jobs, h.nativeLogResolver())
	if err != nil {
		runtimeLogHTTPError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="cooker-native-stage-logs.txt"`)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(data))
}

func (h *Handler) CloseNativeStageLogSession(c *gin.Context) {
	if !h.nativeLogReady(c) {
		return
	}
	if err := h.Runtime.CloseNativeStageLogSession(c.Request.Context(), nativeLogSubject(c), c.Param("sessionId")); err != nil {
		runtimeLogHTTPError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Admission resolves the retained accepted subject before any WS upgrade.
// The server owns the connection nonce and its explicit cancellation lifecycle.
func (h *Handler) PrepareNativeStageLogs(ctx context.Context, subject service.NativeRuntimeSubject) (string, func(context.Context, io.Writer) error, error) {
	if h.Runtime == nil {
		return "", nil, service.ErrRuntimeLogUnavailable
	}
	return h.Runtime.PrepareNativeStageLogs(ctx, subject, h.Jobs, h.nativeLogResolver())
}
