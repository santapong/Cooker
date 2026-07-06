package model

import "time"

// RunStatus represents the state of a pipeline run or stage run.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSuccess   RunStatus = "success"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
	// RunStatusSkipped marks a stage whose incoming edge conditions
	// resolved to "don't run" (dag-adaptation Primitive #2). Terminal,
	// stage-level only — a PipelineRun itself is never skipped.
	RunStatusSkipped RunStatus = "skipped"
)

// RunResult is the terminal outcome the Executor reports back to its
// caller. F2 (docs/audits/2026-05-handler-layering.md Finding 2) moved
// run-status finalisation out of the HTTP handler closure and into
// Execute itself; the handler now persists the result verbatim
// instead of re-deriving terminal state from run.Status.
//
// Status is always terminal — one of RunStatusSuccess,
// RunStatusFailed, or RunStatusCancelled. A non-terminal value on
// return is a programmer error in the executor.
//
// FinishedAt is the wall-clock time at which Execute observed the
// terminal transition. The handler stamps this onto run.FinishedAt
// before persisting; callers should not derive it themselves.
type RunResult struct {
	Status     RunStatus
	FinishedAt time.Time
}

// PipelineRun is a concrete execution of a pipeline definition.
type PipelineRun struct {
	ID                  string              `json:"id" db:"id"`
	PipelineID          string              `json:"pipelineId" db:"pipeline_id"`
	Status              RunStatus           `json:"status" db:"status"`
	StageRuns           []StageRun          `json:"stageRuns"`
	EnvironmentStatuses []EnvironmentStatus `json:"environmentStatuses"`
	Variables           map[string]string   `json:"variables"`
	// CreatedAt is set once at insert time and never changes. Clients use
	// it to sort or paginate run history because StartedAt is null for
	// pending runs.
	CreatedAt  time.Time  `json:"createdAt" db:"created_at"`
	StartedAt  *time.Time `json:"startedAt" db:"started_at"`
	FinishedAt *time.Time `json:"finishedAt" db:"finished_at"`
	Error      string     `json:"error,omitempty" db:"error"`
	// HeartbeatAt is updated by the run coordinator while the run is
	// in flight. A NULL value combined with status='running' marks the
	// row as a candidate for the boot-time orphan sweep.
	HeartbeatAt *time.Time `json:"heartbeatAt,omitempty" db:"heartbeat_at"`
	// StartedBy* captures the actor that initiated the run, so the
	// deploy-stage executor can call the governance gate on their behalf
	// at stage-start time (when the original bearer token is no longer in
	// hand). The token hash is for audit forensics — the raw token is
	// never persisted. Empty StartedByUserSub means the run pre-dates the
	// capture (legacy rows) and the executor skips the governance hook.
	StartedByUserSub   string   `json:"startedByUserSub,omitempty" db:"started_by_user_sub"`
	StartedByEmail     string   `json:"startedByEmail,omitempty" db:"started_by_email"`
	StartedByGroups    []string `json:"startedByGroups,omitempty"`
	StartedByTokenHash string   `json:"startedByTokenHash,omitempty" db:"started_by_token_hash"`
	// PipelineVersion is Pipeline.Version at run-creation time. 0 means
	// "unknown" (rows predating migration 017). Run-diff uses it to
	// report definition drift between two runs.
	PipelineVersion int `json:"pipelineVersion,omitempty" db:"pipeline_version"`
}

// StageRun tracks the execution of a single stage within a pipeline run.
type StageRun struct {
	StageID    string     `json:"stageId"`
	Status     RunStatus  `json:"status"`
	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	Logs       string     `json:"logs,omitempty"`
	Error      string     `json:"error,omitempty"`
	Artifacts  []Artifact `json:"artifacts,omitempty"`
	// Outputs are string key/value pairs a stage exposes for downstream
	// stages to consume via ${stages.<id>.<key>} interpolation. Persisted
	// in the existing stage_runs JSONB column (no migration). Capped:
	// per-key value <= 4 KiB, per-stage total <= 32 KiB.
	Outputs map[string]string `json:"outputs,omitempty"`
}

// Artifact represents a pipeline output, tracked as an OCI content-addressable reference.
type Artifact struct {
	Type   string `json:"type"`   // "oci-image", "test-report", "log"
	Ref    string `json:"ref"`    // e.g., "registry.example.com/app@sha256:abc..."
	Digest string `json:"digest"` // OCI content-addressable digest
}

// EnvironmentStatus tracks deployment status per environment in a pipeline run.
type EnvironmentStatus struct {
	EnvironmentID string     `json:"environmentId"`
	Status        EnvStatus  `json:"status"`
	PromotedAt    *time.Time `json:"promotedAt,omitempty"`
	ApprovedBy    string     `json:"approvedBy,omitempty"`
	// ApprovalsHave / ApprovalsNeed expose manual-gate progress for the
	// run page's promotion lane ("2 of 3 approvals"). Populated by
	// GetEnvStatus from the persisted promotion rows; zero for the
	// executor's legacy inline auto-promote path.
	ApprovalsHave int `json:"approvalsHave,omitempty"`
	ApprovalsNeed int `json:"approvalsNeed,omitempty"`
}

// EnvStatus represents the deployment state for an environment.
type EnvStatus string

const (
	EnvStatusPending          EnvStatus = "pending"
	EnvStatusDeploying        EnvStatus = "deploying"
	EnvStatusDeployed         EnvStatus = "deployed"
	EnvStatusFailed           EnvStatus = "failed"
	EnvStatusAwaitingApproval EnvStatus = "awaiting_approval"
)

// Clone returns a deep copy of the run that is safe to read while the
// original is mutated elsewhere — provided the caller either holds the
// run's owning lock or the original is no longer being written. The
// executor mutates run.StageRuns[i] in place from per-stage goroutines
// during a run, so every concurrent reader (the progress-persistence
// drain, the HTTP response encoder, the in-memory store's Get) must
// snapshot via Clone rather than share the live pointer.
// See docs/proposals/run-state-concurrency-2026.md.
func (r *PipelineRun) Clone() *PipelineRun {
	if r == nil {
		return nil
	}
	// Value copy first: scalars + slice/map headers. The reference-typed
	// fields below are then replaced with independent copies.
	cp := *r
	cp.StartedAt = cloneTimePtr(r.StartedAt)
	cp.FinishedAt = cloneTimePtr(r.FinishedAt)
	cp.HeartbeatAt = cloneTimePtr(r.HeartbeatAt)

	if r.StageRuns != nil {
		cp.StageRuns = make([]StageRun, len(r.StageRuns))
		for i := range r.StageRuns {
			cp.StageRuns[i] = r.StageRuns[i].clone()
		}
	}
	if r.EnvironmentStatuses != nil {
		cp.EnvironmentStatuses = make([]EnvironmentStatus, len(r.EnvironmentStatuses))
		for i := range r.EnvironmentStatuses {
			es := r.EnvironmentStatuses[i]
			es.PromotedAt = cloneTimePtr(r.EnvironmentStatuses[i].PromotedAt)
			cp.EnvironmentStatuses[i] = es
		}
	}
	if r.StartedByGroups != nil {
		cp.StartedByGroups = append([]string(nil), r.StartedByGroups...)
	}
	if r.Variables != nil {
		cp.Variables = make(map[string]string, len(r.Variables))
		for k, v := range r.Variables {
			cp.Variables[k] = v
		}
	}
	return &cp
}

// clone returns a deep copy of a single StageRun (the per-element unit
// the executor mutates in place).
func (s StageRun) clone() StageRun {
	cp := s
	cp.StartedAt = cloneTimePtr(s.StartedAt)
	cp.FinishedAt = cloneTimePtr(s.FinishedAt)
	if s.Artifacts != nil {
		cp.Artifacts = append([]Artifact(nil), s.Artifacts...)
	}
	if s.Outputs != nil {
		cp.Outputs = make(map[string]string, len(s.Outputs))
		for k, v := range s.Outputs {
			cp.Outputs[k] = v
		}
	}
	return cp
}

// cloneTimePtr copies a *time.Time so the snapshot does not alias the
// original pointer's pointee.
func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}
