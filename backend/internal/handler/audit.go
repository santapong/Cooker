package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// Audit-viewer paging bounds. The viewer pages with prev/next, so a
// hard max keeps a single response from dragging the whole table.
const (
	auditListDefaultLimit = 50
	auditListMaxLimit     = 200
)

// ListAuditEvents serves GET /admin/audit (roadmap M5): the queryable
// audit trail behind the db sink. Filters are conjunctive; time bounds
// are RFC3339. Admin-only + MFA (router). Returns 503 until the store
// has audit support (memory store always does; this guards a nil
// Store wiring in tests).
func (h *Handler) ListAuditEvents(c *gin.Context) {
	if h.Store == nil || h.Store.AuditEvents == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit store not configured"})
		return
	}

	q := store.AuditQuery{
		UserSub:    c.Query("user"),
		Method:     c.Query("method"),
		PathPrefix: c.Query("path"),
		Limit:      auditListDefaultLimit,
	}
	if v := c.Query("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "from must be RFC3339 (e.g. 2026-06-01T00:00:00Z)"})
			return
		}
		q.From = &t
	}
	if v := c.Query("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "to must be RFC3339 (e.g. 2026-06-01T00:00:00Z)"})
			return
		}
		q.To = &t
	}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer"})
			return
		}
		if n > auditListMaxLimit {
			n = auditListMaxLimit
		}
		q.Limit = n
	}
	if v := c.Query("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be a non-negative integer"})
			return
		}
		q.Offset = n
	}

	events, err := h.Store.AuditEvents.Query(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if events == nil {
		events = []*model.AuditEvent{}
	}
	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"limit":  q.Limit,
		"offset": q.Offset,
	})
}
