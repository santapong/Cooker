package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/notify/notifier"
	"net/http"
)

func (h *Handler) ListNotificationDeliveries(c *gin.Context) {
	if h.NotificationDeliveries == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Durable notification history requires PostgreSQL; development notifications are best-effort"})
		return
	}
	page, err := h.NotificationDeliveries.ListDeliveries(c.Request.Context(), notifier.DeliveryQuery{
		State: c.Query("state"), TargetID: c.Query("targetId"), RunID: c.Query("runId"),
		Limit: intQuery(c, "limit", listDefaultLimit, 1, listMaxLimit), Offset: intQuery(c, "offset", 0, 0, 1<<30),
	})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Notification delivery history is temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) GetNotificationDelivery(c *gin.Context) {
	if h.NotificationDeliveries == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Durable notification history requires PostgreSQL"})
		return
	}
	d, err := h.NotificationDeliveries.GetDelivery(c.Request.Context(), c.Param("id"))
	if errors.Is(err, notifier.ErrDeliveryNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notification delivery not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Notification delivery history is temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, d)
}
