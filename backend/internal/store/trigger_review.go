package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/santapong/cooker/internal/model"
)

// TriggerReviewStore atomically records exact prospective review and transition.
// Old App transitions remain readable; missing native review never gains proof.
type TriggerReviewStore interface {
	ApproveReviewedIntent(context.Context, string, int, *model.TriggerReview) error
}

func ValidateTriggerReview(i *model.TriggerIntent, r *model.TriggerReview) error {
	reviewObjectMatches := false
	if i != nil && r != nil {
		switch {
		case strings.HasPrefix(i.Event.FullRef, "refs/heads/"):
			// Branch adapters may omit their redundant commit object. A trusted
			// review may preserve it, but cannot attach a different object.
			reviewObjectMatches = r.ObjectSHA == "" || r.ObjectSHA == i.Event.CommitSHA
		case strings.HasPrefix(i.Event.FullRef, "refs/tags/"):
			reviewObjectMatches = i.Event.ObjectSHA != "" && r.ObjectSHA == i.Event.ObjectSHA
		}
	}
	if i == nil || r == nil || i.Status != model.TriggerAwaitingReview || i.Review != nil || r.ReviewerSubject == "" || len(r.ReviewerSubject) > 512 || strings.ContainsAny(r.ReviewerSubject, "\x00\r\n") || r.ReviewedAt.IsZero() || r.IntentVersion != i.Version || r.RuleVersion != i.Rule.Version || r.PipelineVersion != i.Rule.PipelineVersion || r.CommitSHA != i.Event.CommitSHA || !reviewObjectMatches || r.PlanHash != i.Rule.PlanHash || r.EnvelopeHash != i.Rule.ApprovedEnvelopeHash {
		return fmt.Errorf("review does not match the immutable intent: %w", ErrConflict)
	}
	return nil
}
