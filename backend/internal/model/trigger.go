package model

import "time"

// GitSourceIdentity comes from the authenticated connection. RepositoryID is
// the provider's immutable ID; Repository is a display/checkout name only.
type GitSourceIdentity struct {
	Provider     string `json:"provider"`
	ConnectionID string `json:"connectionId"`
	Host         string `json:"host"`
	RepositoryID string `json:"repositoryId"`
	Repository   string `json:"repository,omitempty"`
}

func (s GitSourceIdentity) SameRepository(other GitSourceIdentity) bool {
	return s.Provider == other.Provider && s.ConnectionID == other.ConnectionID && s.Host == other.Host && s.RepositoryID == other.RepositoryID
}

type TriggerRule struct {
	ID                   string            `json:"id"`
	Version              int               `json:"version"`
	Name                 string            `json:"name"`
	Enabled              bool              `json:"enabled"`
	Source               GitSourceIdentity `json:"source"`
	SourceVersion        int               `json:"sourceVersion,omitempty"`
	TargetKind           string            `json:"targetKind,omitempty"`
	TargetID             string            `json:"targetId,omitempty"`
	PipelineVersion      int               `json:"pipelineVersion,omitempty"`
	EnvironmentVersions  map[string]int    `json:"environmentVersions,omitempty"`
	ReviewedBySubject    string            `json:"reviewedBySubject,omitempty"`
	ReviewedAt           *time.Time        `json:"reviewedAt,omitempty"`
	AppID                string            `json:"appId"`
	EnvironmentID        string            `json:"environmentId,omitempty"`
	AppVersion           int               `json:"appVersion,omitempty"`
	EnvironmentVersion   int               `json:"environmentVersion,omitempty"`
	RefKind              string            `json:"refKind"` // branch or tag
	Match                string            `json:"match"`   // exact or glob
	Include              []string          `json:"include"`
	Exclude              []string          `json:"exclude,omitempty"` // same syntax as Include
	Approval             string            `json:"approval"`          // automatic or review
	ActorSubject         string            `json:"actorSubject,omitempty"`
	ActorGroups          []string          `json:"actorGroups,omitempty"`
	ApprovedEnvelopeHash string            `json:"approvedEnvelopeHash,omitempty"`
	PlanHash             string            `json:"planHash,omitempty"`
	CreatedAt            time.Time         `json:"createdAt"`
	UpdatedAt            time.Time         `json:"updatedAt"`
}

// VerifiedGitEvent deliberately excludes raw payloads and authorization
// headers. Only a provider adapter after authentication may construct one.
// CommitSHA is the peeled commit; ObjectSHA preserves an annotated tag object.
type VerifiedGitEvent struct {
	Source      GitSourceIdentity `json:"source"`
	DeliveryID  string            `json:"deliveryId"`
	Event       string            `json:"event"`
	FullRef     string            `json:"fullRef"`
	BeforeSHA   string            `json:"beforeSha,omitempty"`
	ObjectSHA   string            `json:"objectSha,omitempty"`
	CommitSHA   string            `json:"commitSha,omitempty"`
	Deleted     bool              `json:"deleted"`
	PayloadHash string            `json:"payloadHash"`
}

type TriggerIntentStatus string

const (
	TriggerPending           TriggerIntentStatus = "pending"
	TriggerAwaitingReview    TriggerIntentStatus = "awaiting_review"
	TriggerQueued            TriggerIntentStatus = "queued"
	TriggerSuperseded        TriggerIntentStatus = "superseded"
	TriggerFailedPreparation TriggerIntentStatus = "failed_preparation"
)

type TriggerReceipt struct {
	RunID      string `json:"runId"`
	JobID      string `json:"jobId"`
	PipelineID string `json:"pipelineId"`
	PlanDigest string `json:"planDigest"`
}

type TriggerIntent struct {
	ID         string              `json:"id"`
	Version    int                 `json:"version"`
	LogicalKey string              `json:"logicalKey"`
	DeliveryID string              `json:"deliveryId"`
	RunID      string              `json:"runId"` // deterministic; queue admission must reuse it
	Rule       TriggerRule         `json:"rule"`
	Event      VerifiedGitEvent    `json:"event"`
	Status     TriggerIntentStatus `json:"status"`
	Reason     string              `json:"reason"`
	Receipt    *TriggerReceipt     `json:"receipt,omitempty"`
	CreatedAt  time.Time           `json:"createdAt"`
	UpdatedAt  time.Time           `json:"updatedAt"`
	Archived   bool                `json:"archived,omitempty"`
	Review     *TriggerReview      `json:"review,omitempty"`
}

// Target preserves legacy App rules while rejecting contradictory targets at
// validation/admission boundaries. An empty native target is never executable.
func (r TriggerRule) Target() (kind, id string) {
	if r.TargetKind == "" && r.TargetID == "" && r.AppID != "" {
		return "app", r.AppID
	}
	return r.TargetKind, r.TargetID
}

type TriggerDecision struct {
	RuleID        string `json:"ruleId,omitempty"`
	RuleVersion   int    `json:"ruleVersion,omitempty"`
	AppID         string `json:"appId,omitempty"`
	EnvironmentID string `json:"environmentId,omitempty"`
	TargetKind    string `json:"targetKind,omitempty"`
	TargetID      string `json:"targetId,omitempty"`
	Status        string `json:"status"`
	Reason        string `json:"reason"`
	IntentID      string `json:"intentId,omitempty"`
}

type TriggerDelivery struct {
	ID        string            `json:"id"`
	Event     VerifiedGitEvent  `json:"event"`
	Decisions []TriggerDecision `json:"decisions"`
	CreatedAt time.Time         `json:"createdAt"`
	Archived  bool              `json:"archived,omitempty"`
}
