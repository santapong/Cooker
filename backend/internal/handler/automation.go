package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func (h *Handler) requireAutomation(c *gin.Context) bool {
	if h.Automation == nil || h.Automation.Triggers == nil || h.Automation.Triggers.Store == nil {
		c.JSON(503, gin.H{"error": "durable source automation is unavailable"})
		return false
	}
	return true
}
func (h *Handler) ListTriggerRules(c *gin.Context) {
	if !h.requireAutomation(c) {
		return
	}
	rules, err := h.Automation.Triggers.Store.ListRules(c.Request.Context(), c.Query("connectionId"))
	if err != nil {
		c.JSON(503, gin.H{"error": "cannot load trigger rules"})
		return
	}
	if rules == nil {
		rules = []*model.TriggerRule{}
	}
	c.JSON(200, rules)
}

type triggerRequest struct {
	Rule                 model.TriggerRule `json:"rule"`
	Ref                  string            `json:"ref"`
	ExpectedEnvelopeHash string            `json:"expectedEnvelopeHash"`
	ExpectedPlanHash     string            `json:"expectedPlanHash"`
}

func (h *Handler) PreviewTriggerRule(c *gin.Context) { h.prepareTriggerRule(c, false) }
func (h *Handler) SaveTriggerRule(c *gin.Context)    { h.prepareTriggerRule(c, true) }
func (h *Handler) prepareTriggerRule(c *gin.Context, save bool) {
	if !h.requireAutomation(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var req triggerRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid trigger rule"})
		return
	}
	if req.Rule.ID == "" {
		req.Rule.ID = uuid.NewString()
	}
	if _, err := uuid.Parse(req.Rule.ID); err != nil {
		c.JSON(400, gin.H{"error": "invalid rule ID"})
		return
	}
	if kind, _ := req.Rule.Target(); save && kind == "pipeline" {
		if user := auth.GetUser(c); user == nil || user.Subject == "" {
			c.JSON(401, gin.H{"error": "authenticated reviewer is required"})
			return
		}
	}
	// Never accept client-provided machine authority or approval evidence.
	req.Rule.ActorSubject = ""
	req.Rule.ActorGroups = nil
	req.Rule.ApprovedEnvelopeHash = ""
	req.Rule.PlanHash = ""
	req.Rule.ReviewedBySubject = ""
	req.Rule.ReviewedAt = nil
	req.Rule.Source = model.GitSourceIdentity{ConnectionID: req.Rule.Source.ConnectionID}
	req.Rule.SourceVersion = 0
	req.Rule.AppVersion = 0
	req.Rule.EnvironmentID = ""
	req.Rule.EnvironmentVersion = 0
	req.Rule.PipelineVersion = 0
	req.Rule.EnvironmentVersions = nil
	preview, err := h.Automation.Preview(c.Request.Context(), &req.Rule, req.Ref)
	if err != nil {
		c.JSON(422, gin.H{"error": "cannot prepare this rule; verify the source, environment and deployment configuration", "code": "trigger_review_unavailable"})
		return
	}
	if !save {
		c.JSON(200, preview)
		return
	}
	if req.ExpectedEnvelopeHash != preview.Rule.ApprovedEnvelopeHash || req.ExpectedPlanHash != preview.Rule.PlanHash {
		c.JSON(409, gin.H{"error": "configuration changed or has not been reviewed; preview this rule before saving"})
		return
	}
	if kind, _ := preview.Rule.Target(); kind == "pipeline" {
		user := auth.GetUser(c)
		if user == nil || user.Subject == "" {
			c.JSON(401, gin.H{"error": "authenticated reviewer is required"})
			return
		}
		now := time.Now().UTC()
		preview.Rule.ReviewedBySubject = user.Subject
		preview.Rule.ReviewedAt = &now
	}
	if err := h.Automation.Triggers.SaveRule(c.Request.Context(), preview.Rule); err != nil {
		code := 503
		if errors.Is(err, store.ErrConflict) {
			code = 409
		}
		c.JSON(code, gin.H{"error": "cannot save trigger rule; refresh and review it again"})
		return
	}
	c.JSON(200, preview.Rule)
}

func (h *Handler) DisableTriggerRule(c *gin.Context) {
	if !h.requireAutomation(c) {
		return
	}
	r, err := h.Automation.Triggers.Store.GetRule(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Status(404)
		return
	}
	r.Enabled = false
	if err := h.Automation.Triggers.SaveRule(c.Request.Context(), r); err != nil {
		c.JSON(409, gin.H{"error": "rule changed; refresh and try again"})
		return
	}
	c.JSON(200, r)
}
func (h *Handler) GetTriggerDelivery(c *gin.Context) {
	if !h.requireAutomation(c) {
		return
	}
	d, err := h.Automation.Triggers.Store.GetDelivery(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Status(404)
		return
	}
	c.JSON(200, d)
}
func (h *Handler) GetTriggerIntent(c *gin.Context) {
	if !h.requireAutomation(c) {
		return
	}
	i, err := h.Automation.Triggers.Store.GetIntent(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Status(404)
		return
	}
	c.JSON(200, i)
}
func (h *Handler) ApproveTriggerIntent(c *gin.Context) {
	if !h.requireAutomation(c) {
		return
	}
	user := auth.GetUser(c)
	if user == nil || user.Subject == "" {
		c.JSON(401, gin.H{"error": "authenticated reviewer is required"})
		return
	}
	i, err := h.Automation.Triggers.Store.GetIntent(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Status(404)
		return
	}
	if i.Status != model.TriggerAwaitingReview {
		c.JSON(409, gin.H{"error": "intent is not awaiting review"})
		return
	}
	if err := h.Automation.ApproveIntent(c.Request.Context(), i, user.Subject); err != nil {
		c.JSON(409, gin.H{"error": "cannot approve this intent; refresh and review its source and configuration", "code": "trigger_approval_unavailable"})
		return
	}
	c.JSON(202, gin.H{"intentId": i.ID, "status": "pending", "message": "Approved for final source and configuration checks"})
}
func (h *Handler) CheckTriggerRef(c *gin.Context) {
	if !h.requireAutomation(c) {
		return
	}
	var req struct {
		Ref string `json:"ref"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "choose a full branch or tag ref"})
		return
	}
	r, err := h.Automation.Triggers.Store.GetRule(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Status(404)
		return
	}
	d, dup, err := h.Automation.CheckRef(c.Request.Context(), r, req.Ref)
	if err != nil {
		c.JSON(422, gin.H{"error": "cannot verify the selected ref"})
		return
	}
	c.JSON(202, gin.H{"delivery": d, "duplicate": dup})
}

func (h *Handler) ListTriggerIntents(c *gin.Context) {
	if !h.requireAutomation(c) {
		return
	}
	items, err := h.Automation.Triggers.Store.ListIntents(c.Request.Context(), c.Query("appId"), 100)
	if err != nil {
		c.JSON(503, gin.H{"error": "cannot read trigger history"})
		return
	}
	if items == nil {
		items = []*model.TriggerIntent{}
	}
	c.JSON(200, items)
}
