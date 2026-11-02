package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/store"
)

func (h *Handler) ListTriggerDeliveries(c *gin.Context)     { h.triggerHistory(c, false) }
func (h *Handler) GetTriggerDeliveryHistory(c *gin.Context) { h.triggerHistory(c, true) }

func (h *Handler) triggerHistory(c *gin.Context, detail bool) {
	if !h.requireAutomation(c) {
		return
	}
	history, ok := h.Automation.Triggers.Store.(store.TriggerHistoryStore)
	if !ok {
		c.JSON(503, gin.H{"error": "delivery browsing is unavailable"})
		return
	}
	query := store.TriggerHistoryQuery{AppID: c.Query("appId"), Provider: c.Query("provider"), ConnectionID: c.Query("connectionId"), Status: c.Query("status"), Ref: c.Query("ref"), Search: c.Query("q"), DeliveryID: c.Query("deliveryId"), TargetKind: c.Query("targetKind"), TargetID: c.Query("targetId")}
	if detail {
		query.DeliveryID = c.Param("id")
	}
	for key, dst := range map[string]*int{"limit": &query.Limit, "offset": &query.Offset} {
		if value, present := c.GetQuery(key); present {
			n, err := strconv.Atoi(value)
			if err != nil {
				c.JSON(400, gin.H{"error": "invalid history pagination"})
				return
			}
			*dst = n
		}
	}
	if strings.HasPrefix(query.AppID, "vapp_") || strings.HasPrefix(query.TargetID, "vapp_") || strings.HasPrefix(query.TargetID, "vflow_") {
		c.JSON(400, gin.H{"error": "Git deployment history applies to native Cooker targets; imported provider flows are read-only"})
		return
	}
	query, err := query.Normalize()
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	page, err := history.ListTriggerHistory(c.Request.Context(), query)
	if err != nil {
		c.JSON(503, gin.H{"error": "cannot read delivery history"})
		return
	}
	if detail && page.Total == 0 {
		if _, err := h.Automation.Triggers.Store.GetDelivery(c.Request.Context(), query.DeliveryID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				c.Status(404)
			} else {
				c.JSON(503, gin.H{"error": "cannot read delivery history"})
			}
			return
		}
	}
	c.JSON(200, page)
}
