package model

import "time"

// PromotionStatus is the lifecycle state of a single run→environment
// promotion. It is a superset of EnvStatus: a promotion can sit in
// "awaiting_approval" until its manual gate collects enough distinct
// approvers, then advance to "approved" and on to deploying/deployed.
type PromotionStatus string

const (
	// PromotionPending is the initial state before any gate evaluation.
	PromotionPending PromotionStatus = "pending"
	// PromotionAwaitingApproval is a manual gate that has not yet
	// collected RequiredApprovers distinct approvals.
	PromotionAwaitingApproval PromotionStatus = "awaiting_approval"
	// PromotionApproved is a manual gate whose approval threshold has
	// been met; the deploy may proceed.
	PromotionApproved PromotionStatus = "approved"
	// PromotionDeploying marks an approved (or auto) promotion whose
	// deploy is in flight.
	PromotionDeploying PromotionStatus = "deploying"
	// PromotionDeployed is the terminal success state.
	PromotionDeployed PromotionStatus = "deployed"
	// PromotionFailed is the terminal failure state.
	PromotionFailed PromotionStatus = "failed"
)

// RunPromotion is a persisted promotion of a pipeline run to one target
// environment. strategy and RequiredApprovers are snapshotted from the
// target environment's PromotionPolicy at creation time, so editing the
// policy afterwards does not retroactively change an in-flight gate.
// See docs/adr/0005-promotion-approval-persistence.md.
type RunPromotion struct {
	ID            string `json:"id" db:"id"`
	RunID         string `json:"runId" db:"run_id"`
	PipelineID    string `json:"pipelineId,omitempty" db:"pipeline_id"`
	EnvironmentID string `json:"environmentId" db:"environment_id"`

	Status PromotionStatus `json:"status" db:"status"`
	// Strategy is "auto" or "manual", snapshotted from the env policy.
	Strategy string `json:"strategy" db:"strategy"`
	// RequiredApprovers is the distinct-approver threshold for a manual
	// gate, snapshotted from the env policy. Ignored for auto.
	RequiredApprovers int `json:"requiredApprovers" db:"required_approvers"`
	// RequestedBy is the identity (email) that initiated the promotion.
	RequestedBy string `json:"requestedBy,omitempty" db:"requested_by"`

	// PromotedAt is set when the promotion advances past the gate
	// (immediately for auto; at threshold for manual).
	PromotedAt *time.Time `json:"promotedAt,omitempty" db:"promoted_at"`
	CreatedAt  time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt  time.Time  `json:"updatedAt" db:"updated_at"`

	// Approvals is the set of approvals recorded against this promotion.
	// Populated on reads that ask for it (GetByRunEnv, ListByRun); empty
	// otherwise. Not a stored column.
	Approvals []PromotionApproval `json:"approvals,omitempty"`
}

// ApprovalCount returns the number of distinct approvals recorded.
func (p *RunPromotion) ApprovalCount() int { return len(p.Approvals) }

// HasApprover reports whether the given subject has already approved.
func (p *RunPromotion) HasApprover(sub string) bool {
	for i := range p.Approvals {
		if p.Approvals[i].ApproverSub == sub {
			return true
		}
	}
	return false
}

// PromotionApproval is one approval recorded against a RunPromotion.
// Append-only; the (PromotionID, ApproverSub) pair is unique so a
// double-click by the same approver counts once.
type PromotionApproval struct {
	ID            string    `json:"id" db:"id"`
	PromotionID   string    `json:"promotionId" db:"promotion_id"`
	ApproverSub   string    `json:"approverSub,omitempty" db:"approver_sub"`
	ApproverEmail string    `json:"approverEmail" db:"approver_email"`
	Note          string    `json:"note,omitempty" db:"note"`
	CreatedAt     time.Time `json:"createdAt" db:"created_at"`
}

// ToEnvironmentStatus projects a promotion onto the EnvironmentStatus
// shape the run page's promotion lane consumes. The approver field
// carries the most recent approver's email (best-effort, for display);
// ApprovalsHave/ApprovalsNeed expose the full gate progress.
func (p *RunPromotion) ToEnvironmentStatus() EnvironmentStatus {
	es := EnvironmentStatus{
		EnvironmentID: p.EnvironmentID,
		Status:        EnvStatus(p.Status),
		PromotedAt:    p.PromotedAt,
		ApprovalsHave: len(p.Approvals),
		ApprovalsNeed: p.RequiredApprovers,
	}
	if n := len(p.Approvals); n > 0 {
		es.ApprovedBy = p.Approvals[n-1].ApproverEmail
	}
	return es
}
