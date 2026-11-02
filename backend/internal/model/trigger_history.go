package model

import "time"

// TriggerHistoryRow is a presentation allowlist. It never embeds an event,
// intent, rule, job payload, environment, actor, or provider error.
type TriggerHistoryRow struct {
	DeliveryID         string    `json:"deliveryId"`
	ProviderDeliveryID string    `json:"providerDeliveryId"`
	Provider           string    `json:"provider"`
	ConnectionID       string    `json:"connectionId"`
	Repository         string    `json:"repository,omitempty"`
	FullRef            string    `json:"fullRef,omitempty"`
	CommitSHA          string    `json:"commitSha,omitempty"`
	Deleted            bool      `json:"deleted"`
	RuleID             string    `json:"ruleId,omitempty"`
	RuleVersion        int       `json:"ruleVersion,omitempty"`
	AppID              string    `json:"appId,omitempty"`
	EnvironmentID      string    `json:"environmentId,omitempty"`
	TargetKind         string    `json:"targetKind,omitempty"`
	TargetID           string    `json:"targetId,omitempty"`
	MetadataCoverage   string    `json:"metadataCoverage,omitempty"`
	TargetSource       string    `json:"targetSource,omitempty"`
	IntentID           string    `json:"intentId,omitempty"`
	IntentStatus       string    `json:"intentStatus,omitempty"`
	JobID              string    `json:"jobId,omitempty"`
	JobStatus          string    `json:"jobStatus,omitempty"`
	RunID              string    `json:"runId,omitempty"`
	RunStatus          string    `json:"runStatus,omitempty"`
	PipelineID         string    `json:"pipelineId,omitempty"`
	Status             string    `json:"status"`
	ReasonCode         string    `json:"reasonCode"`
	Reason             string    `json:"reason"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
	Archived           bool      `json:"archived"`
	DecisionIndex      int       `json:"decisionIndex"`
	// Internal projection inputs are never serialized and are cleared before return.
	StoredStatus     string `json:"-"`
	StoredReason     string `json:"-"`
	StoredReasonCode string `json:"-"`
}

type TriggerHistoryPage struct {
	Items            []TriggerHistoryRow `json:"items"`
	Total            int                 `json:"total"`
	Limit            int                 `json:"limit"`
	Offset           int                 `json:"offset"`
	Coverage         string              `json:"coverage"`
	Limitations      []string            `json:"limitations"`
	JournalStartedAt *time.Time          `json:"journalStartedAt,omitempty"`
}
