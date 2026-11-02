package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/santapong/cooker/internal/model"
)

// This optional read interface leaves admission and execution stores unchanged.
type TriggerHistoryStore interface {
	ListTriggerHistory(context.Context, TriggerHistoryQuery) (*model.TriggerHistoryPage, error)
}

type TriggerHistoryQuery struct {
	AppID, Provider, ConnectionID, Status, Ref, Search, DeliveryID string
	TargetKind, TargetID                                           string
	Limit, Offset                                                  int
}

func (q TriggerHistoryQuery) Normalize() (TriggerHistoryQuery, error) {
	for _, s := range []string{q.AppID, q.Provider, q.ConnectionID, q.Status, q.Ref, q.Search, q.DeliveryID, q.TargetKind, q.TargetID} {
		if len(s) > 1024 || strings.ContainsAny(s, "\x00\r\n") {
			return q, fmt.Errorf("invalid history filter")
		}
	}
	if len(q.Search) > 256 {
		return q, fmt.Errorf("search is too long")
	}
	if q.TargetKind != "" && q.TargetKind != "app" && q.TargetKind != "pipeline" {
		return q, fmt.Errorf("unsupported history target")
	}
	if q.Provider != "" && q.Provider != "github" && q.Provider != "gitlab" && q.Provider != "gitea" {
		return q, fmt.Errorf("unsupported source provider")
	}
	switch q.Status {
	case "", "accepted", "ignored", "duplicate", "stale", "rejected", "review_required", "failed", "archived", "unknown":
	default:
		return q, fmt.Errorf("unsupported history status")
	}
	if q.Limit == 0 {
		q.Limit = 25
	}
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 1000000 {
		return q, fmt.Errorf("invalid history pagination")
	}
	return q, nil
}

func NewTriggerHistoryPage(q TriggerHistoryQuery) *model.TriggerHistoryPage {
	return &model.TriggerHistoryPage{Items: []model.TriggerHistoryRow{}, Limit: q.Limit, Offset: q.Offset, Coverage: "authenticated_admitted_decisions", Limitations: []string{
		"This view lists admitted decisions. Authenticated retry/rejection processing is available separately in webhook attempts; authentication failures are not journaled.",
		"Before the audit cutover, compacted ignored/duplicate decisions may be unavailable and cannot be reconstructed. Prospective minimal decisions survive compaction.",
		"Legacy ignored decisions without a recorded immutable target use the rule's current App, explicitly labelled current rule. Prospective decisions retain their original target.",
	}}
}

func TriggerHistoryStatus(decision, intent, job, run string, archived bool) string {
	if decision == "duplicate" {
		return "duplicate"
	}
	if decision == "ignored" || decision == "rejected" {
		return decision
	}
	if intent == "queued" {
		if job == "failed" || run == "failed" {
			return "failed"
		}
		if job == "needs_attention" {
			return "review_required"
		}
	}
	status := decision
	if intent != "" {
		status = intent
	}
	switch status {
	case "pending", "queued", "accepted":
		return "accepted"
	case "awaiting_review", "review_required":
		return "review_required"
	case "superseded", "stale":
		return "stale"
	case "failed_preparation", "failed":
		return "failed"
	case "rejected":
		return "rejected"
	case "unknown":
		return "unknown"
	}
	if archived {
		return "archived"
	}
	return "unknown"
}

// Reasons are intentionally allowlisted: old preparation errors can contain
// private file/environment data. Unknown errors become actionable safe text.
var triggerHistorySafeReasons = map[string]string{
	"repository does not match": "repository_mismatch", "rule is disabled": "rule_disabled", "ref kind does not match": "ref_kind_mismatch", "ref matches an exclusion": "ref_excluded", "ref does not match": "ref_mismatch", "no trigger rules for this connection": "no_rules", "event does not trigger deployment": "unsupported_event", "ref deleted; deployed workloads are unchanged": "ref_deleted", "release tag moved; verify the new revision before deployment": "tag_moved", "source ref changed; this delivery will not deploy a different revision": "source_changed", "source ref cannot be verified; retry after reviewing provider access": "source_unavailable", "rule changed or was disabled; review a new intent": "rule_changed", "app, environment, or connection changed; review the automation settings": "configuration_changed", "event source is outside the approved repository": "source_outside_approval", "automation actor is no longer authorized": "actor_not_authorized", "deployment configuration changed; inspect and approve its new configuration": "deployment_configuration_changed",
}

func TriggerReasonForCode(code string) string {
	for text, known := range triggerHistorySafeReasons {
		if known == code {
			return text
		}
	}
	return ""
}

func PresentTriggerHistory(row model.TriggerHistoryRow) model.TriggerHistoryRow {
	row.Status = TriggerHistoryStatus(row.StoredStatus, row.IntentStatus, row.JobStatus, row.RunStatus, row.Archived)
	reason := row.StoredReason
	if reason == "" && row.StoredReasonCode != "" {
		reason = TriggerReasonForCode(row.StoredReasonCode)
	}
	row.StoredStatus = ""
	row.StoredReason = ""
	row.StoredReasonCode = ""
	if code, ok := triggerHistorySafeReasons[reason]; ok {
		row.ReasonCode = code
		row.Reason = reason
	} else {
		row.ReasonCode = row.Status
		switch row.Status {
		case "duplicate":
			row.Reason = "This rule, revision and configuration already have a deployment intent. No additional job was created by this decision."
		case "ignored":
			row.Reason = "This authenticated event did not match an active deployment rule."
		case "stale":
			row.Reason = "The accepted source revision was superseded; this intent will not deploy a different revision."
		case "rejected":
			row.Reason = "Deployment admission rejected this decision. Review the rule and approved configuration."
		case "review_required":
			row.Reason = "Deployment requires review of source access, actor permissions or configuration before it can proceed."
		case "failed":
			row.Reason = "Preparation or deployment failed. Inspect the linked run when available; private error details are omitted here."
		case "accepted":
			if row.IntentID != "" && row.IntentStatus == "" {
				row.Reason = "An intent was recorded, but its current deployment details are unavailable. No rollout outcome is inferred."
			} else if row.IntentStatus == "queued" {
				row.Reason = "Deployment durably queued. Queue acceptance does not confirm a successful rollout."
			} else {
				row.Reason = "Intent recorded; source, actor and deployment configuration still require validation. Deployment has not started."
			}
		case "archived":
			row.Reason = "The delivery identity is retained, but its original decisions were compacted."
		default:
			row.Reason = "A historical decision is recorded, but its status is not supported by this version."
		}
	}
	// Only known lifecycle values are displayed, never arbitrary stored errors.
	switch row.IntentStatus {
	case "", "pending", "awaiting_review", "queued", "superseded", "failed_preparation":
	default:
		row.IntentStatus = "unknown"
	}
	switch row.JobStatus {
	case "", "pending", "running", "succeeded", "failed", "cancelled", "needs_attention":
	default:
		row.JobStatus = "unknown"
	}
	switch row.RunStatus {
	case "", "pending", "running", "success", "failed", "cancelled":
	default:
		row.RunStatus = "unknown"
	}
	return row
}

func TriggerHistoryMatches(row model.TriggerHistoryRow, q TriggerHistoryQuery) bool {
	if q.TargetKind != "" && row.TargetKind != q.TargetKind || q.TargetID != "" && row.TargetID != q.TargetID {
		return false
	}
	if q.AppID != "" && q.AppID != row.AppID || q.Provider != "" && q.Provider != row.Provider || q.ConnectionID != "" && q.ConnectionID != row.ConnectionID || q.Status != "" && q.Status != row.Status || q.Ref != "" && q.Ref != row.FullRef || q.DeliveryID != "" && q.DeliveryID != row.DeliveryID {
		return false
	}
	if q.Search == "" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.Join([]string{row.DeliveryID, row.ProviderDeliveryID, row.Provider, row.Repository, row.FullRef, row.CommitSHA, row.RuleID, row.AppID, row.TargetKind, row.TargetID, row.IntentID, row.JobID, row.RunID}, " ")), strings.ToLower(q.Search))
}
