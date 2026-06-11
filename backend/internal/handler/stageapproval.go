package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/service"
)

// requireStageApprovals aborts with 503 when the stage-approval service
// was not wired (defensive — server.New always sets it; nil means a
// store-less test Handler). Keeps the handler from panicking on nil.
func (h *Handler) requireStageApprovals(c *gin.Context) bool {
	if h.StageApprovals != nil {
		return true
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error": "stage approval service unavailable",
	})
	return false
}

// ListStageApprovals returns all approval gates for a run (awaiting +
// resolved). Any authenticated user may read them — the run page polls
// this to show which approval stages are paused. The gate body carries no
// secret material. HS26-05-03.
func (h *Handler) ListStageApprovals(c *gin.Context) {
	if !h.requireStageApprovals(c) {
		return
	}
	run, ok := h.loadRunForPipeline(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	gates, err := h.StageApprovals.List(c.Request.Context(), run.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"runId": run.ID,
		"gates": gates,
	})
}

// ApproveStage records an approval for a run's approval-gate stage.
// Requires the caller to hold RoleAdmin or RoleApprover (same RBAC as
// promotion approval); the approver identity is taken from the OIDC
// claims, not the request body (IDOR-safe). The gate advances — and the
// blocked executor resumes the stage — once the distinct-approver count
// reaches the stage's RequiredApprovers. HS26-05-03.
func (h *Handler) ApproveStage(c *gin.Context) {
	claims := auth.GetUser(c)
	if !auth.CanApprovePromotion(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "requires admin or approver role",
		})
		return
	}
	if !h.requireStageApprovals(c) {
		return
	}

	run, ok := h.loadRunForPipeline(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	stageID := c.Param("stageId")
	var req struct {
		Note string `json:"note"`
	}
	// Body is optional (only a note); ignore a bind error on an empty body.
	_ = c.ShouldBindJSON(&req)

	gate, err := h.StageApprovals.Approve(c.Request.Context(), run.ID, stageID, claims.Subject, claims.Email, req.Note)
	if err != nil && !errors.Is(err, service.ErrAlreadyApproved) {
		if errors.Is(err, service.ErrGateResolved) {
			c.JSON(http.StatusConflict, gin.H{"error": "gate already resolved", "status": gate.Status})
			return
		}
		if abortStoreErr(c, err, "approval gate not found") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"runId":         run.ID,
		"stageId":       stageID,
		"status":        gate.Status,
		"approvedBy":    claims.Email,
		"note":          req.Note,
		"approvalsHave": gate.ApprovalCount(),
		"approvalsNeed": gate.RequiredApprovers,
		"gate":          gate,
	})
}

// RejectStage rejects a run's approval-gate stage. A single rejection
// settles the gate regardless of RequiredApprovers (one veto is enough);
// the blocked executor then fails the stage. Same RBAC and identity rules
// as ApproveStage. HS26-05-03.
func (h *Handler) RejectStage(c *gin.Context) {
	claims := auth.GetUser(c)
	if !auth.CanApprovePromotion(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "requires admin or approver role",
		})
		return
	}
	if !h.requireStageApprovals(c) {
		return
	}

	run, ok := h.loadRunForPipeline(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	stageID := c.Param("stageId")
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)

	gate, err := h.StageApprovals.Reject(c.Request.Context(), run.ID, stageID, claims.Email, req.Note)
	if err != nil {
		if errors.Is(err, service.ErrGateResolved) {
			c.JSON(http.StatusConflict, gin.H{"error": "gate already resolved", "status": gate.Status})
			return
		}
		if abortStoreErr(c, err, "approval gate not found") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"runId":      run.ID,
		"stageId":    stageID,
		"status":     gate.Status,
		"rejectedBy": claims.Email,
		"note":       req.Note,
		"gate":       gate,
	})
}
