package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/scheduler"
)

// adminScheduleRequest is the JSON body of POST /admin/schedules
// and PUT /admin/schedules/:id. Cron expression is validated through
// scheduler.Parse before persistence so an operator typo surfaces
// as 400 rather than silently disabling the row at runtime.
type adminScheduleRequest struct {
	PipelineID string `json:"pipelineId" binding:"required"`
	Name       string `json:"name,omitempty"`
	CronExpr   string `json:"cronExpr" binding:"required"`
	Timezone   string `json:"timezone,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

// validateScheduleRequest runs cross-cutting validation common to
// create + update. Mirrors the failure modes the scheduler runner
// would otherwise hit at firing time.
func validateScheduleRequest(req *adminScheduleRequest) error {
	if _, err := uuid.Parse(req.PipelineID); err != nil {
		return errors.New("pipelineId must be a UUID")
	}
	if _, err := scheduler.Parse(req.CronExpr); err != nil {
		return err
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return err
	}
	return nil
}

// ListSchedules returns every schedule row. Admin-only.
func (h *Handler) ListSchedules(c *gin.Context) {
	if h.Schedules == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not configured"})
		return
	}
	list, err := h.Schedules.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []scheduler.Schedule{}
	}
	c.JSON(http.StatusOK, list)
}

// GetSchedule returns one schedule by ID.
func (h *Handler) GetSchedule(c *gin.Context) {
	if h.Schedules == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not configured"})
		return
	}
	s, err := h.Schedules.Get(c.Request.Context(), c.Param("id"))
	if errors.Is(err, scheduler.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

// CreateSchedule registers a new cron-triggered run. NextRunAt is
// computed from the cron expression in the schedule's timezone so
// the scheduler tick picks it up at the right moment.
func (h *Handler) CreateSchedule(c *gin.Context) {
	if h.Schedules == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not configured"})
		return
	}
	var req adminScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateScheduleRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	s := scheduler.Schedule{
		ID:         uuid.New().String(),
		PipelineID: req.PipelineID,
		Name:       req.Name,
		CronExpr:   req.CronExpr,
		Timezone:   req.Timezone,
		Enabled:    enabled,
	}
	nextRun, err := nextRunFromSchedule(s)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.NextRunAt = nextRun
	if err := h.Schedules.Create(c.Request.Context(), s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, s)
}

// UpdateSchedule replaces a schedule by ID. NextRunAt is recomputed
// from the (possibly changed) cron expression / timezone; LastRunAt
// / LastRunID are preserved so the operator's edit doesn't reset the
// firing history.
func (h *Handler) UpdateSchedule(c *gin.Context) {
	if h.Schedules == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not configured"})
		return
	}
	id := c.Param("id")
	existing, err := h.Schedules.Get(c.Request.Context(), id)
	if errors.Is(err, scheduler.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var req adminScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateScheduleRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.PipelineID = req.PipelineID
	existing.Name = req.Name
	existing.CronExpr = req.CronExpr
	existing.Timezone = req.Timezone
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	nextRun, err := nextRunFromSchedule(existing)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.NextRunAt = nextRun
	if err := h.Schedules.Update(c.Request.Context(), existing); err != nil {
		if errors.Is(err, scheduler.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, existing)
}

// DeleteSchedule removes a schedule by ID.
func (h *Handler) DeleteSchedule(c *gin.Context) {
	if h.Schedules == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not configured"})
		return
	}
	err := h.Schedules.Delete(c.Request.Context(), c.Param("id"))
	if errors.Is(err, scheduler.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// nextRunFromSchedule is a thin wrapper that picks the next firing
// time from the cron expression in the schedule's location. Kept
// here (vs. on scheduler.Schedule) so we don't widen the package
// surface; the runner has its own equivalent path for the tick loop.
func nextRunFromSchedule(s scheduler.Schedule) (time.Time, error) {
	cron, err := scheduler.Parse(s.CronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return cron.Next(time.Now(), s.LoadLocation())
}
