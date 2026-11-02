package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type TriggerService struct{ Store store.TriggerStore }

func ValidateTriggerRule(r *model.TriggerRule) error {
	if r == nil || r.ID == "" {
		return fmt.Errorf("trigger rule requires an ID and target")
	}
	kind, target := r.Target()
	switch kind {
	case "app":
		if target == "" || (r.AppID != "" && r.AppID != target) || r.PipelineVersion != 0 || len(r.EnvironmentVersions) != 0 {
			return fmt.Errorf("contradictory App trigger target")
		}
		r.AppID = target
	case "pipeline":
		if target == "" || r.AppID != "" || r.AppVersion != 0 || r.EnvironmentID != "" || r.EnvironmentVersion != 0 || strings.HasPrefix(target, "vflow_") || strings.HasPrefix(target, "vapp_") {
			return fmt.Errorf("native pipeline trigger target is required")
		}
		if r.PipelineVersion < 0 || len(r.EnvironmentVersions) > 100 {
			return fmt.Errorf("invalid pipeline trigger versions")
		}
		for id, version := range r.EnvironmentVersions {
			if id == "" || len(id) > 128 || strings.ContainsAny(id, "\x00\r\n") || version < 0 {
				return fmt.Errorf("invalid pipeline environment version")
			}
		}
	default:
		return fmt.Errorf("choose App or native pipeline target")
	}
	if err := validateTriggerSource(r.Source); err != nil {
		return err
	}
	if len(target) > 128 || strings.ContainsAny(target, "\x00\r\n") || len(r.ReviewedBySubject) > 512 || strings.ContainsAny(r.ReviewedBySubject, "\x00\r\n") || len(r.ID) > 128 || len(r.Name) > 128 || len(r.AppID) > 128 || len(r.EnvironmentID) > 128 || len(r.ActorSubject) > 512 || len(r.ActorGroups) > 32 || r.Version < 0 || r.SourceVersion < 0 || r.AppVersion < 0 || r.EnvironmentVersion < 0 {
		return fmt.Errorf("trigger rule metadata exceeds supported limits")
	}
	for _, group := range r.ActorGroups {
		if len(group) > 128 || strings.ContainsAny(group, "\x00\r\n") {
			return fmt.Errorf("invalid trigger actor group")
		}
	}
	for _, hash := range []string{r.PlanHash, r.ApprovedEnvelopeHash} {
		if hash != "" && !validHex(hash, 64) {
			return fmt.Errorf("plan and envelope hashes must be SHA-256 values")
		}
	}
	if r.RefKind != "branch" && r.RefKind != "tag" {
		return fmt.Errorf("choose branch or tag")
	}
	if r.Match != "exact" && r.Match != "glob" {
		return fmt.Errorf("choose exact or glob matching")
	}
	if r.Approval == "" {
		r.Approval = "review"
	}
	if r.Approval != "automatic" && r.Approval != "review" {
		return fmt.Errorf("choose automatic or review approval")
	}
	if len(r.Include) == 0 || len(r.Include) > 32 || len(r.Exclude) > 32 {
		return fmt.Errorf("use 1–32 includes and at most 32 exclusions")
	}
	for _, patterns := range [][]string{r.Include, r.Exclude} {
		for _, pattern := range patterns {
			if len(pattern) == 0 || len(pattern) > 512 || strings.ContainsAny(pattern, "\x00\r\n") || strings.HasPrefix(pattern, "refs/") {
				return fmt.Errorf("patterns must be short ref names, without refs/ prefix")
			}
			if r.Match == "glob" {
				if strings.Contains(pattern, "**") {
					return fmt.Errorf("globstar ** is unsupported; * matches within one slash-separated segment")
				}
				if _, err := path.Match(pattern, ""); err != nil {
					return fmt.Errorf("invalid glob pattern %q", pattern)
				}
			}
		}
	}
	return nil
}

func validateTriggerSource(s model.GitSourceIdentity) error {
	for _, v := range []string{s.Provider, s.ConnectionID, s.Host, s.RepositoryID} {
		if v == "" || len(v) > 512 || strings.ContainsAny(v, "\x00\r\n") {
			return fmt.Errorf("trigger source identity is incomplete or invalid")
		}
	}
	if len(s.Repository) > 512 {
		return fmt.Errorf("repository name is too long")
	}
	return nil
}

func (s *TriggerService) SaveRule(ctx context.Context, r *model.TriggerRule) error {
	if err := ValidateTriggerRule(r); err != nil {
		return err
	}
	return s.Store.PutRule(ctx, r)
}

// TriggerRuleMatches compares authenticated provider/host/repository identity
// exactly, then matches a short branch/tag name. Exclusions win. Glob syntax is
// path.Match: *, ?, character classes; * never crosses a slash; no **.
func TriggerRuleMatches(rule *model.TriggerRule, event model.VerifiedGitEvent) (bool, string) {
	if rule == nil {
		return false, "rule is unavailable"
	}
	return rule.Matches(event)
}

func validHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateVerifiedGitEvent(e model.VerifiedGitEvent) error {
	if err := validateTriggerSource(e.Source); err != nil {
		return err
	}
	if e.DeliveryID == "" || len(e.DeliveryID) > 256 || strings.ContainsAny(e.DeliveryID, "\x00\r\n") {
		return fmt.Errorf("verified delivery ID is required")
	}
	if !validHex(e.PayloadHash, 64) {
		return fmt.Errorf("verified payload SHA-256 is required")
	}
	if len(e.Event) > 64 || len(e.FullRef) > 1024 || len(e.BeforeSHA) > 40 || len(e.ObjectSHA) > 40 || len(e.CommitSHA) > 40 {
		return fmt.Errorf("event metadata exceeds supported limits")
	}
	if e.BeforeSHA != "" && !validHex(e.BeforeSHA, 40) {
		return fmt.Errorf("invalid prior revision SHA")
	}
	if e.Event == "push" && !e.Deleted {
		if !validHex(e.CommitSHA, 40) || strings.Trim(e.CommitSHA, "0") == "" {
			return fmt.Errorf("push requires a verified full commit SHA")
		}
		if e.ObjectSHA != "" && !validHex(e.ObjectSHA, 40) {
			return fmt.Errorf("invalid ref object SHA")
		}
		if (!strings.HasPrefix(e.FullRef, "refs/heads/") && !strings.HasPrefix(e.FullRef, "refs/tags/")) || len(e.FullRef) > 1024 || strings.ContainsAny(e.FullRef, "\x00\r\n ") {
			return fmt.Errorf("push requires a full branch or tag ref")
		}
	}
	return nil
}

// ValidateVerifiedGitEvent classifies authenticated invalid input before inbox
// admission; callers must never persist its free-form error as audit data.
func ValidateVerifiedGitEvent(e model.VerifiedGitEvent) error { return validateVerifiedGitEvent(e) }

// AcceptVerified only records decisions and durable intents. Queue admission
// separately validates actor authority and the inspected deployment envelope.
// A pending record is not a claim that any deployment has started.
func (s *TriggerService) AcceptVerified(ctx context.Context, event model.VerifiedGitEvent) (*model.TriggerDelivery, bool, error) {
	if s == nil || s.Store == nil {
		return nil, false, fmt.Errorf("trigger inbox is unavailable")
	}
	if err := validateVerifiedGitEvent(event); err != nil {
		return nil, false, err
	}
	delivery := &model.TriggerDelivery{ID: TriggerDeliveryID(event.Source.Provider, event.Source.ConnectionID, event.DeliveryID), Event: event, CreatedAt: time.Now().UTC()}
	if event.Event != "push" || event.Deleted {
		reason := "event does not trigger deployment"
		if event.Deleted {
			reason = "ref deleted; deployed workloads are unchanged"
		}
		delivery.Decisions = []model.TriggerDecision{{Status: "ignored", Reason: reason}}
		return s.Store.RecordDelivery(ctx, delivery, nil)
	}
	rules, err := s.Store.ListRules(ctx, event.Source.ConnectionID)
	if err != nil {
		return nil, false, err
	}
	var intents []*model.TriggerIntent
	for _, rule := range rules {
		matched, reason := TriggerRuleMatches(rule, event)
		kind, target := rule.Target()
		decision := model.TriggerDecision{RuleID: rule.ID, RuleVersion: rule.Version, AppID: rule.AppID, EnvironmentID: rule.EnvironmentID, TargetKind: kind, TargetID: target, Status: "ignored", Reason: reason}
		if matched {
			// Include target/config/approval-envelope identity, not just the SHA.
			keyInput, _ := json.Marshal(struct {
				Rule                *model.TriggerRule
				Ref, Commit, Object string
			}{rule, event.FullRef, event.CommitSHA, event.ObjectSHA})
			keyHash := sha256.Sum256(keyInput)
			logicalKey := hex.EncodeToString(keyHash[:])
			intentID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("intent:"+logicalKey)).String()
			status := model.TriggerPending
			reason = "awaiting source, actor and deployment-envelope validation"
			if rule.Approval != "automatic" || rule.ActorSubject == "" || rule.ApprovedEnvelopeHash == "" || (kind == "pipeline" && (rule.ReviewedBySubject == "" || rule.ReviewedAt == nil)) {
				status = model.TriggerAwaitingReview
				reason = "deployment requires an approved actor and configuration envelope"
			}
			intent := &model.TriggerIntent{ID: intentID, Version: 1, LogicalKey: logicalKey, DeliveryID: delivery.ID, RunID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("cooker-intent:"+intentID)).String(), Rule: *rule, Event: event, Status: status, Reason: reason, CreatedAt: delivery.CreatedAt, UpdatedAt: delivery.CreatedAt}
			intents = append(intents, intent)
			decision.Status = string(status)
			decision.Reason = reason
			decision.IntentID = intentID
		}
		delivery.Decisions = append(delivery.Decisions, decision)
	}
	if len(rules) == 0 {
		delivery.Decisions = []model.TriggerDecision{{Status: "ignored", Reason: "no trigger rules for this connection"}}
	}
	return s.Store.RecordDelivery(ctx, delivery, intents)
}

// TriggerDeliveryID is shared by authenticated retry lookup and inbox insertion.
func TriggerDeliveryID(provider, connectionID, deliveryID string) string {
	identity := provider + "\x00" + connectionID + "\x00" + deliveryID
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("delivery:"+identity)).String()
}
