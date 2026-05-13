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
)

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
