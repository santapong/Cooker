package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/store"
)

func (h *Handler) ListWebhookAttempts(c *gin.Context) {
	if !h.requireAutomation(c) {
		return
	}
	journal, ok := h.Automation.Triggers.Store.(store.WebhookAttemptStore)
	if !ok {
		c.JSON(503, gin.H{"error": "authenticated delivery audit is unavailable"})
		return
	}
	q := store.WebhookAttemptQuery{AppID: c.Query("appId"), Provider: c.Query("provider"), ConnectionID: c.Query("connectionId"), Status: c.Query("status"), Search: c.Query("q"), DeliveryID: c.Query("deliveryId")}
	for key, dst := range map[string]*int{"limit": &q.Limit, "offset": &q.Offset} {
		if raw, ok := c.GetQuery(key); ok {
			n, err := strconv.Atoi(raw)
			if err != nil {
				c.JSON(400, gin.H{"error": "invalid attempt pagination"})
				return
			}
			*dst = n
		}
	}
	if strings.HasPrefix(q.AppID, "vapp_") {
		c.JSON(400, gin.H{"error": "Git webhook audit applies to Cooker Apps; imported provider flows are read-only"})
		return
	}
	q, err := q.Normalize()
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	page, err := journal.ListWebhookAttempts(c.Request.Context(), q)
	if err != nil {
		c.JSON(503, gin.H{"error": "cannot read authenticated webhook attempts"})
		return
	}
	c.JSON(200, page)
}
