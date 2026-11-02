package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/jobqueue"
)

func (h *Handler) GetJob(c *gin.Context) {
	if h.Jobs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "durable job queue is not enabled"})
		return
	}
	j, err := h.Jobs.Get(c.Request.Context(), c.Param("id"))
	if errors.Is(err, jobqueue.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot read job"})
		return
	}
	// The execution payload may contain private configuration. Never echo it.
	j.Payload = nil
	c.JSON(http.StatusOK, j)
}

// ResolveJob records a checked external outcome; it does not rerun deployment.
// The router requires administrator access and configured MFA.
func (h *Handler) ResolveJob(c *gin.Context) {
	leases, ok := h.Jobs.(jobqueue.LeaseStore)
	atomic, atomicOK := h.Jobs.(interface{ AtomicLifecycle() bool })
	ok = ok && atomicOK && atomic.AtomicLifecycle()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "job recovery is unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		Outcome  jobqueue.Status `json:"outcome"`
		Evidence string          `json:"evidence"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resolution"})
		return
	}
	if (req.Outcome != jobqueue.StatusSucceeded && req.Outcome != jobqueue.StatusFailed && req.Outcome != jobqueue.StatusCancelled) || len(req.Evidence) < 10 || len(req.Evidence) > 1600 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "record a terminal outcome and 10–1600 characters describing what you verified"})
		return
	}
	j, err := h.Jobs.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	if j.Status != jobqueue.StatusNeedsAttention {
		c.JSON(http.StatusConflict, gin.H{"error": "job does not need reconciliation"})
		return
	}
	actor := "local administrator"
	if u := auth.GetUser(c); u != nil {
		actor = u.Subject
	}
	evidence := "Verified by " + actor + ": " + req.Evidence

	if err := leases.Resolve(c.Request.Context(), j.ID, req.Outcome, evidence); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "cannot resolve job; reload its current state"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobId": j.ID, "status": req.Outcome, "message": "verified outcome recorded; a new deployment may now run"})
}
