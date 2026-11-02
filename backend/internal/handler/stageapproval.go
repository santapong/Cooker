package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
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

// Approval reads and decisions must not return run-store diagnostics. Keep
// absent and differently owned runs indistinguishable, and retain the existing
// conflict response without exposing an underlying storage or context error.
func (h *Handler) loadRunForStageApproval(c *gin.Context, runID, pipelineID string) (*model.PipelineRun, bool) {
	run, err := h.Store.Runs.Get(c.Request.Context(), runID)
	if errors.Is(err, store.ErrNotFound) {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return nil, false
	}
	if errors.Is(err, store.ErrConflict) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "version conflict; refetch and retry"})
		return nil, false
	}
	if err != nil || run == nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "approval run could not be loaded"})
		return nil, false
	}
	if run.PipelineID != pipelineID {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return nil, false
	}
	return run, true
}

// ListStageApprovals returns all approval gates for a run (awaiting +
// resolved). Any authenticated user may read them — the run page polls
// this to show which approval stages are paused. The gate body carries no
// secret material. HS26-05-03.
func (h *Handler) ListStageApprovals(c *gin.Context) {
	if !h.requireStageApprovals(c) {
		return
	}
	run, ok := h.loadRunForStageApproval(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	gates, err := h.StageApprovals.List(c.Request.Context(), run.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stage approvals could not be loaded"})
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

	run, ok := h.loadRunForStageApproval(c, c.Param("runId"), c.Param("id"))
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
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "approval gate not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stage approval could not be recorded; refresh its current status before retrying"})
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

	run, ok := h.loadRunForStageApproval(c, c.Param("runId"), c.Param("id"))
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
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "approval gate not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stage rejection could not be recorded; refresh its current status before retrying"})
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
