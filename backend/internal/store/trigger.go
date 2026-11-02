package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/model"
)

var ErrDeliveryConflict = errors.New("webhook delivery identity was reused with different authenticated content")

type TriggerStore interface {
	// PutRule creates with Version=0 or updates with the current Version.
	PutRule(context.Context, *model.TriggerRule) error
	GetRule(context.Context, string) (*model.TriggerRule, error)
	ListRules(ctx context.Context, connectionID string) ([]*model.TriggerRule, error)
	// RecordDelivery commits the inbox and all logical intents atomically.
	// Existing delivery IDs return duplicate=true only when identity agrees.
	RecordDelivery(context.Context, *model.TriggerDelivery, []*model.TriggerIntent) (delivery *model.TriggerDelivery, duplicate bool, err error)
	GetDelivery(context.Context, string) (*model.TriggerDelivery, error)
	GetIntent(context.Context, string) (*model.TriggerIntent, error)
	// Recent intents of every status, optionally filtered by App; capped at 100.
	ListIntents(ctx context.Context, appID string, limit int) ([]*model.TriggerIntent, error)
	PendingIntents(context.Context, int) ([]*model.TriggerIntent, error)
	// TransitionIntent uses an expected version. queued requires its receipt.
	TransitionIntent(ctx context.Context, id string, version int, status model.TriggerIntentStatus, reason string, receipt *model.TriggerReceipt) error
	// Compact old terminal audit records, retaining dedupe identities forever.
	PruneTriggers(context.Context, time.Time) (int64, error)
}

func SameTriggerDelivery(a, b *model.TriggerDelivery) bool {
	return reflect.DeepEqual(a.Event, b.Event)
}

func CloneTrigger[T any](value T) T {
	b, _ := json.Marshal(value)
	var cp T
	_ = json.Unmarshal(b, &cp)
	return cp
}

func ValidateTriggerTransition(current *model.TriggerIntent, status model.TriggerIntentStatus, reason string, receipt *model.TriggerReceipt) error {
	if len(reason) > 2048 {
		return fmt.Errorf("trigger reason is too long")
	}
	if current.Status != model.TriggerPending && current.Status != model.TriggerAwaitingReview {
		return ErrConflict
	}
	switch status {
	case model.TriggerPending:
		if current.Status != model.TriggerAwaitingReview {
			return ErrConflict
		}
	case model.TriggerQueued:
		if current.Status != model.TriggerPending || receipt == nil || receipt.RunID != current.RunID || receipt.JobID == "" || receipt.PipelineID == "" || receipt.PlanDigest == "" {
			return fmt.Errorf("queued intent requires its matching durable admission receipt")
		}
	case model.TriggerAwaitingReview, model.TriggerFailedPreparation, model.TriggerSuperseded:
	default:
		return fmt.Errorf("unsupported trigger transition")
	}
	return nil
}

func ApplyTriggerIntentDecision(delivery *model.TriggerDelivery, intent *model.TriggerIntent, duplicate bool, requestedID ...string) {
	needle := intent.ID
	if len(requestedID) > 0 {
		needle = requestedID[0]
	}
	for i := range delivery.Decisions {
		d := &delivery.Decisions[i]
		if d.IntentID != needle {
			continue
		}
		d.IntentID = intent.ID
		d.Status = string(intent.Status)
		d.Reason = intent.Reason
		if duplicate {
			d.Status = "duplicate"
			d.Reason = "this rule, revision and configuration already have a deployment intent"
		}
	}
}

func CompactTriggerIntent(i *model.TriggerIntent) {
	r := i.Rule
	i.Rule = model.TriggerRule{ID: r.ID, Version: r.Version, SourceVersion: r.SourceVersion, TargetKind: r.TargetKind, TargetID: r.TargetID, PipelineVersion: r.PipelineVersion, EnvironmentVersions: r.EnvironmentVersions, ReviewedBySubject: r.ReviewedBySubject, ReviewedAt: r.ReviewedAt, AppID: r.AppID, EnvironmentID: r.EnvironmentID, AppVersion: r.AppVersion, EnvironmentVersion: r.EnvironmentVersion, PlanHash: r.PlanHash, ApprovedEnvelopeHash: r.ApprovedEnvelopeHash}
	i.Archived = true
}

func TriggerTagObject(event model.VerifiedGitEvent) string {
	if event.ObjectSHA != "" {
		return event.ObjectSHA
	}
	return event.CommitSHA
}

func TriggerTagMovedByEvent(event model.VerifiedGitEvent) bool {
	return event.Event == "push" && strings.HasPrefix(event.FullRef, "refs/tags/") && event.BeforeSHA != "" && strings.Trim(event.BeforeSHA, "0") != "" && event.BeforeSHA != TriggerTagObject(event)
}
