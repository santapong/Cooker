package model

import "time"

// StageApprovalStatus is the lifecycle state of a single pipeline
// approval-gate stage (StageTypeApproval). The gate is created
// "awaiting" when the executor reaches the stage, advances to "approved"
// once RequiredApprovers distinct approvals land (the executor then
// resumes the run), or to "rejected" when any approver rejects (the
// executor then fails the stage). Mirrors PromotionStatus but with a
// rejection terminal, since an approval gate is a binary human decision.
type StageApprovalStatus string

const (
	// StageApprovalAwaiting is the initial gate state: paused, waiting for
	// approvers.
	StageApprovalAwaiting StageApprovalStatus = "awaiting"
	// StageApprovalApproved means the distinct-approver threshold was met;
	// the executor resumes the stage as succeeded.
	StageApprovalApproved StageApprovalStatus = "approved"
	// StageApprovalRejected means an approver rejected the gate; the
	// executor fails the stage.
	StageApprovalRejected StageApprovalStatus = "rejected"
)

// StageApproval is a persisted human-in-the-loop gate for one approval
// stage within a pipeline run, keyed uniquely by (run, stage). It mirrors
// RunPromotion's relational-rows approach (one gate row + append-only
// distinct-approver votes) so the gate survives a restart and the
// approval count is enforced at the DB level. See migration 022 and
// docs/adr/0005-promotion-approval-persistence.md for the shared design.
type StageApproval struct {
	ID      string `json:"id" db:"id"`
	RunID   string `json:"runId" db:"run_id"`
	StageID string `json:"stageId" db:"stage_id"`

	Status StageApprovalStatus `json:"status" db:"status"`
	// RequiredApprovers is the distinct-approver threshold, snapshotted
	// from the stage's config at gate-creation time (default 1).
	RequiredApprovers int `json:"requiredApprovers" db:"required_approvers"`
	// ResolvedBy is the email of the approver/rejecter that settled the
	// gate (most recent vote for an approval; the rejecter for a rejection).
	ResolvedBy string `json:"resolvedBy,omitempty" db:"resolved_by"`

	// ResolvedAt is set when the gate advances past awaiting.
	ResolvedAt *time.Time `json:"resolvedAt,omitempty" db:"resolved_at"`
	CreatedAt  time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt  time.Time  `json:"updatedAt" db:"updated_at"`

	// Votes is the set of approval votes recorded against this gate.
	// Populated on reads that ask for it; empty otherwise. Not a stored
	// column. A rejection short-circuits the gate, so a rejected gate may
	// have fewer votes than RequiredApprovers.
	Votes []StageApprovalVote `json:"votes,omitempty"`
}

// ApprovalCount returns the number of distinct approval votes recorded.
func (a *StageApproval) ApprovalCount() int { return len(a.Votes) }

// HasApprover reports whether the given subject has already voted.
func (a *StageApproval) HasApprover(sub string) bool {
	for i := range a.Votes {
		if a.Votes[i].ApproverSub == sub {
			return true
		}
	}
	return false
}

// StageApprovalVote is one approval recorded against a StageApproval.
// Append-only; the (StageApprovalID, ApproverSub) pair is unique so a
// double-click by the same approver counts once. Only approvals are
// recorded as votes — a rejection settles the gate directly.
type StageApprovalVote struct {
	ID              string    `json:"id" db:"id"`
	StageApprovalID string    `json:"stageApprovalId" db:"stage_approval_id"`
	ApproverSub     string    `json:"approverSub,omitempty" db:"approver_sub"`
	ApproverEmail   string    `json:"approverEmail" db:"approver_email"`
	Note            string    `json:"note,omitempty" db:"note"`
	CreatedAt       time.Time `json:"createdAt" db:"created_at"`
}
