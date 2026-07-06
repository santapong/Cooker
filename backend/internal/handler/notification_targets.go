package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/notifier"
	"github.com/santapong/cooker/internal/validate"
)

// adminNotificationTargetRequest is the JSON body of POST /admin/
// notification-targets and PUT /admin/notification-targets/:id.
// Config is channel-specific (Slack URL, SMTP host, etc.) and is
// stored opaquely; the channel adapter parses it on send. Empty
// EventTypes means "all events" — matches notifier.Target.Matches.
type adminNotificationTargetRequest struct {
	Name       string               `json:"name" binding:"required"`
	Kind       string               `json:"kind" binding:"required"`
	Config     json.RawMessage      `json:"config" binding:"required"`
	EventTypes []notifier.EventType `json:"eventTypes,omitempty"`
	Enabled    *bool                `json:"enabled,omitempty"`
}

// defaultCreateEventTypes is the failure/state-change set a new
// target subscribes to when the request omits eventTypes. Defaulting
// to the wildcard (every event, including every green run) is the
// alert-fatigue anti-pattern — operators mute a noisy channel and
// then miss the failure it existed for. An operator who genuinely
// wants everything can pass an explicit list including the success
// events. Existing rows keep their stored semantics unchanged.
var defaultCreateEventTypes = []notifier.EventType{
	notifier.EventRunFailed,
	notifier.EventDeployFailed,
	notifier.EventBuildFailed,
	notifier.EventCanaryFailed,
}

func validateTargetRequest(req *adminNotificationTargetRequest) error {
	if err := validate.Name("name", req.Name); err != nil {
		return err
	}
	switch req.Kind {
	case "slack", "discord", "email", "webhook":
	default:
		return errors.New("kind must be one of: slack, discord, email, webhook")
	}
	if len(req.Config) == 0 {
		return errors.New("config is required")
	}
	if !json.Valid(req.Config) {
		return errors.New("config must be valid JSON")
	}
	return nil
}

// ListNotificationTargets returns every configured target. Admin-only.
func (h *Handler) ListNotificationTargets(c *gin.Context) {
	if h.NotificationTargets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifier not configured"})
		return
	}
	list, err := h.NotificationTargets.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []notifier.Target{}
	}
	c.JSON(http.StatusOK, list)
}

// GetNotificationTarget returns one target by ID.
func (h *Handler) GetNotificationTarget(c *gin.Context) {
	if h.NotificationTargets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifier not configured"})
		return
	}
	t, err := h.NotificationTargets.Get(c.Request.Context(), c.Param("id"))
	if errors.Is(err, notifier.ErrTargetNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "target not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// CreateNotificationTarget registers a new notification destination.
func (h *Handler) CreateNotificationTarget(c *gin.Context) {
	if h.NotificationTargets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifier not configured"})
		return
	}
	var req adminNotificationTargetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateTargetRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	eventTypes := req.EventTypes
	if len(eventTypes) == 0 {
		// Omitted → failure/state-change set, not the wildcard.
		eventTypes = defaultCreateEventTypes
	}
	t := notifier.Target{
		ID:         uuid.New().String(),
		Name:       req.Name,
		Kind:       req.Kind,
		Config:     req.Config,
		EventTypes: eventTypes,
		Enabled:    enabled,
	}
	if err := h.NotificationTargets.Create(c.Request.Context(), t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, t)
}

// UpdateNotificationTarget replaces a target by ID.
func (h *Handler) UpdateNotificationTarget(c *gin.Context) {
	if h.NotificationTargets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifier not configured"})
		return
	}
	id := c.Param("id")
	existing, err := h.NotificationTargets.Get(c.Request.Context(), id)
	if errors.Is(err, notifier.ErrTargetNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "target not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var req adminNotificationTargetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateTargetRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.Name = req.Name
	existing.Kind = req.Kind
	existing.Config = req.Config
	existing.EventTypes = req.EventTypes
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if err := h.NotificationTargets.Update(c.Request.Context(), existing); err != nil {
		if errors.Is(err, notifier.ErrTargetNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "target not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, existing)
}

// DeleteNotificationTarget removes a target by ID.
func (h *Handler) DeleteNotificationTarget(c *gin.Context) {
	if h.NotificationTargets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifier not configured"})
		return
	}
	err := h.NotificationTargets.Delete(c.Request.Context(), c.Param("id"))
	if errors.Is(err, notifier.ErrTargetNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "target not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
