package handler

import (
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetServiceRuntime returns a snapshot of the live container/pod state
// for one deployed compose service of an app. Powers the deployment-
// view runtime panel header. 503 when no RuntimeService is wired.
func (h *Handler) GetServiceRuntime(c *gin.Context) {
	if h.Runtime == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "runtime inspection not configured"})
		return
	}
	app, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if err != nil || app == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
		return
	}
	svc := c.Param("svc")
	status, err := h.Runtime.Status(c.Request.Context(), app, svc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, status)
}

// RuntimeLogsProducer returns the producer closure the WS hub uses to
// tail a service's live logs. Kept on the handler so server wiring can
// look up the app and bind the RuntimeService without leaking store
// access into the hub. Returns nil + false when the app is unknown or
// no RuntimeService is configured.
func (h *Handler) RuntimeLogsProducer(appID, serviceID string) (func(ctx context.Context, out io.Writer) error, bool) {
	if h.Runtime == nil {
		return nil, false
	}
	app, err := h.Store.Apps.Get(context.Background(), appID)
	if err != nil || app == nil {
		return nil, false
	}
	return func(ctx context.Context, out io.Writer) error {
		return h.Runtime.Logs(ctx, app, serviceID, out)
	}, true
}
