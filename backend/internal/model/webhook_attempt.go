package model

import "time"

// WebhookAttempt deliberately contains no raw body/header, provider error,
// source authority, credentials or payload digest. An attempt is not a run.
type WebhookAttempt struct {
	ID                 string     `json:"id"`
	Provider           string     `json:"provider"`
	ConnectionID       string     `json:"connectionId"`
	ProviderDeliveryID string     `json:"providerDeliveryId,omitempty"`
	DeliveryID         string     `json:"deliveryId,omitempty"`
	Status             string     `json:"status"`
	ReasonCode         string     `json:"reasonCode"`
	CreatedAt          time.Time  `json:"createdAt"`
	FinishedAt         *time.Time `json:"finishedAt,omitempty"`
}

type WebhookAttemptPage struct {
	Items            []WebhookAttempt `json:"items"`
	Total            int              `json:"total"`
	Limit            int              `json:"limit"`
	Offset           int              `json:"offset"`
	JournalStartedAt time.Time        `json:"journalStartedAt"`
	Coverage         string           `json:"coverage"`
	Limitations      []string         `json:"limitations"`
}

// Minimal decisions remain independent of rich inbox/spec retention.
type TriggerDecisionRecord struct {
	DeliveryID    string
	DecisionIndex int
	RuleID        string
	RuleVersion   int
	AppID         string
	EnvironmentID string
	TargetKind    string
	TargetID      string
	IntentID      string
	Outcome       string
	ReasonCode    string
}
