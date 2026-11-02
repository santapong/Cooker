package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func triggerReviewFixture() (*model.TriggerIntent, *model.TriggerReview) {
	commit := strings.Repeat("a", 40)
	i := &model.TriggerIntent{Version: 3, Status: model.TriggerAwaitingReview, Rule: model.TriggerRule{Version: 4, PipelineVersion: 7, PlanHash: strings.Repeat("b", 64), ApprovedEnvelopeHash: strings.Repeat("c", 64)}, Event: model.VerifiedGitEvent{FullRef: "refs/heads/main", CommitSHA: commit}}
	r := &model.TriggerReview{ReviewerSubject: "server-reviewer", ReviewedAt: time.Now().UTC(), IntentVersion: i.Version, RuleVersion: i.Rule.Version, PipelineVersion: i.Rule.PipelineVersion, CommitSHA: commit, ObjectSHA: commit, PlanHash: i.Rule.PlanHash, EnvelopeHash: i.Rule.ApprovedEnvelopeHash}
	return i, r
}

func TestTriggerReviewTypedRefObjects(t *testing.T) {
	commit, tag := strings.Repeat("a", 40), strings.Repeat("d", 40)
	for _, tc := range []struct {
		name, ref, eventObject, reviewObject string
		accept                               bool
	}{
		{"branch-resolved-commit", "refs/heads/main", "", commit, true},
		{"branch-omitted-object", "refs/heads/main", "", "", true},
		{"branch-event-repeats-commit", "refs/heads/main", commit, commit, true},
		{"branch-wrong-review-object", "refs/heads/main", "", tag, false},
		{"branch-malformed-review-object", "refs/heads/main", "", "synthetic-secret-object", false},
		{"annotated-tag", "refs/tags/v1", tag, tag, true},
		{"lightweight-tag", "refs/tags/v1", commit, commit, true},
		{"tag-missing-event-object", "refs/tags/v1", "", tag, false},
		{"tag-missing-review-object", "refs/tags/v1", tag, "", false},
		{"tag-same-commit-other-object", "refs/tags/v1", tag, commit, false},
		{"untyped-ref", "main", "", commit, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, r := triggerReviewFixture()
			i.Event.FullRef, i.Event.ObjectSHA, r.ObjectSHA = tc.ref, tc.eventObject, tc.reviewObject
			err := ValidateTriggerReview(i, r)
			if (err == nil) != tc.accept || !tc.accept && (!errors.Is(err, ErrConflict) || strings.Contains(err.Error(), "synthetic-secret-object")) {
				t.Fatal("review typed-ref authority changed", err)
			}
		})
	}
}

func TestTriggerReviewPreservesEveryExistingApprovalGuard(t *testing.T) {
	for name, mutate := range map[string]func(*model.TriggerIntent, *model.TriggerReview){
		"not-awaiting-review": func(i *model.TriggerIntent, _ *model.TriggerReview) { i.Status = model.TriggerPending },
		"already-reviewed":    func(i *model.TriggerIntent, r *model.TriggerReview) { i.Review = r },
		"missing-reviewer":    func(_ *model.TriggerIntent, r *model.TriggerReview) { r.ReviewerSubject = "" },
		"oversized-reviewer":  func(_ *model.TriggerIntent, r *model.TriggerReview) { r.ReviewerSubject = strings.Repeat("a", 513) },
		"invalid-reviewer": func(_ *model.TriggerIntent, r *model.TriggerReview) {
			r.ReviewerSubject = "synthetic-secret-reviewer\n"
		},
		"missing-observation": func(_ *model.TriggerIntent, r *model.TriggerReview) { r.ReviewedAt = time.Time{} },
		"intent-version":      func(_ *model.TriggerIntent, r *model.TriggerReview) { r.IntentVersion++ },
		"rule-version":        func(_ *model.TriggerIntent, r *model.TriggerReview) { r.RuleVersion++ },
		"pipeline-version":    func(_ *model.TriggerIntent, r *model.TriggerReview) { r.PipelineVersion++ },
		"commit":              func(_ *model.TriggerIntent, r *model.TriggerReview) { r.CommitSHA = strings.Repeat("e", 40) },
		"plan":                func(_ *model.TriggerIntent, r *model.TriggerReview) { r.PlanHash = strings.Repeat("e", 64) },
		"recipe":              func(_ *model.TriggerIntent, r *model.TriggerReview) { r.EnvelopeHash = strings.Repeat("e", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			i, r := triggerReviewFixture()
			mutate(i, r)
			err := ValidateTriggerReview(i, r)
			if !errors.Is(err, ErrConflict) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("approval guard was weakened or private metadata echoed", err)
			}
		})
	}
	_, r := triggerReviewFixture()
	if !errors.Is(ValidateTriggerReview(nil, r), ErrConflict) || !errors.Is(ValidateTriggerReview(&model.TriggerIntent{}, nil), ErrConflict) {
		t.Fatal("missing review or intent gained authority")
	}
}
