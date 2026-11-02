package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/service"
)

// GetNativeStageRuntime exposes read-only observation of a retained native
// deployment. Only the path selects its subject; the service verifies the
// immutable accepted inputs and the server-recorded full runtime identity.
func (h *Handler) GetNativeStageRuntime(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if h.Runtime == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": service.ErrNativeRuntimeUnavailable.Error()})
		return
	}
	subject := service.NativeRuntimeSubject{Kind: "native-stage", PipelineID: c.Param("id"), RunID: c.Param("runId"), StageID: c.Param("stageId")}
	status, err := h.Runtime.NativeStageStatus(c.Request.Context(), subject, h.Jobs)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, status)
	case errors.Is(err, service.ErrNativeRuntimeRequest):
		c.JSON(http.StatusBadRequest, gin.H{"error": service.ErrNativeRuntimeRequest.Error()})
	case errors.Is(err, service.ErrNativeRuntimeNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": service.ErrNativeRuntimeNotFound.Error()})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": service.ErrNativeRuntimeUnavailable.Error()})
	}
}
