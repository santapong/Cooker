package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/santapong/cooker/internal/model"
)

type WebhookAttemptStore interface {
	BeginWebhookAttempt(context.Context, *model.WebhookAttempt) error
	FinishWebhookAttempt(context.Context, string, string, string, string) error
	ListWebhookAttempts(context.Context, WebhookAttemptQuery) (*model.WebhookAttemptPage, error)
}

type WebhookAttemptQuery struct {
	AppID, Provider, ConnectionID, Status, Search, DeliveryID string
	Limit, Offset                                             int
}

func SafeWebhookTransportID(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func (q WebhookAttemptQuery) Normalize() (WebhookAttemptQuery, error) {
	base, err := (TriggerHistoryQuery{AppID: q.AppID, Provider: q.Provider, ConnectionID: q.ConnectionID, Search: q.Search, DeliveryID: q.DeliveryID, Limit: q.Limit, Offset: q.Offset}).Normalize()
	if err != nil {
		return q, err
	}
	switch q.Status {
	case "", "received", "accepted", "ignored", "duplicate", "rejected", "failed", "uncertain":
	default:
		return q, fmt.Errorf("unsupported attempt status")
	}
	q.Limit = base.Limit
	q.Offset = base.Offset
	return q, nil
}

func ValidateWebhookAttempt(a *model.WebhookAttempt) error {
	if a == nil || a.ID == "" || a.ConnectionID == "" || len(a.ID) > 128 || len(a.ConnectionID) > 512 || len(a.ProviderDeliveryID) > 256 || strings.ContainsAny(a.ID+a.ConnectionID+a.ProviderDeliveryID, "\x00\r\n") {
		return fmt.Errorf("invalid attempt identity")
	}
	if a.ProviderDeliveryID != "" && !SafeWebhookTransportID(a.ProviderDeliveryID) {
		return fmt.Errorf("invalid attempt transport identity")
	}
	if a.Provider != "github" && a.Provider != "gitlab" && a.Provider != "gitea" {
		return fmt.Errorf("unsupported attempt provider")
	}
	if a.Status != "received" || a.ReasonCode != "processing" || a.DeliveryID != "" || a.FinishedAt != nil {
		return fmt.Errorf("attempt must start in received state")
	}
	return nil
}

func ValidateWebhookAttemptOutcome(status, code string) error {
	want := map[string]string{"admitted": "accepted", "already_recorded": "duplicate", "unsupported_event": "ignored", "invalid_delivery_identity": "rejected", "invalid_json": "rejected", "invalid_event": "rejected", "repository_not_allowed": "rejected", "installation_mismatch": "rejected", "tag_identity_unverified": "rejected", "tag_changed": "rejected", "delivery_identity_conflict": "rejected", "tag_source_unavailable": "failed", "tag_revision_unavailable": "failed", "history_unavailable": "failed", "admission_unconfirmed": "uncertain"}
	if want[code] != status || status == "" {
		return fmt.Errorf("invalid authenticated attempt outcome")
	}
	return nil
}

func NewWebhookAttemptPage(q WebhookAttemptQuery, cutover time.Time) *model.WebhookAttemptPage {
	return &model.WebhookAttemptPage{Items: []model.WebhookAttempt{}, Limit: q.Limit, Offset: q.Offset, JournalStartedAt: cutover, Coverage: "prospective_authenticated_connection_requests", Limitations: []string{"Only requests handled by the upgraded durable Git connection webhook after authentication are journaled. Earlier attempts, legacy handler versions and other webhook routes cannot be reconstructed.", "Authentication failures and oversized requests rejected before authentication are not journaled; raw request bodies and headers are never retained.", "Received means processing began without a confirmed terminal audit result; uncertain means inbox admission could not be confirmed. Neither authorizes replay or proves a deployment outcome."}}
}

func MinimalTriggerDecision(delivery string, index int, d model.TriggerDecision) model.TriggerDecisionRecord {
	view := PresentTriggerHistory(model.TriggerHistoryRow{StoredStatus: d.Status, StoredReason: d.Reason})
	return model.TriggerDecisionRecord{DeliveryID: delivery, DecisionIndex: index, RuleID: d.RuleID, RuleVersion: d.RuleVersion, AppID: d.AppID, EnvironmentID: d.EnvironmentID, TargetKind: d.TargetKind, TargetID: d.TargetID, IntentID: d.IntentID, Outcome: view.Status, ReasonCode: view.ReasonCode}
}

func WebhookAttemptMatches(a model.WebhookAttempt, q WebhookAttemptQuery) bool {
	if q.Provider != "" && q.Provider != a.Provider || q.ConnectionID != "" && q.ConnectionID != a.ConnectionID || q.Status != "" && q.Status != a.Status || q.DeliveryID != "" && q.DeliveryID != a.DeliveryID {
		return false
	}
	return q.Search == "" || strings.Contains(strings.ToLower(strings.Join([]string{a.ID, a.Provider, a.ConnectionID, a.ProviderDeliveryID, a.DeliveryID}, " ")), strings.ToLower(q.Search))
}
