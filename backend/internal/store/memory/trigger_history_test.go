package memory

import (
	"context"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestMemoryTriggerHistoryCurrentDecisionAndInputIndependence(t *testing.T) {
	ctx := context.Background()
	s := NewTriggerStore()
	rule := &model.TriggerRule{ID: "rule", AppID: "app"}
	if err := s.PutRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	d := &model.TriggerDelivery{ID: "delivery", CreatedAt: time.Now(), Event: model.VerifiedGitEvent{Source: model.GitSourceIdentity{Provider: "gitea", ConnectionID: "connection", Repository: "team/repo"}, DeliveryID: "provider-id", FullRef: "refs/heads/main"}, Decisions: []model.TriggerDecision{{RuleID: rule.ID, RuleVersion: rule.Version, IntentID: "intent", Status: "pending"}, {RuleID: rule.ID, RuleVersion: rule.Version, Status: "ignored", Reason: "ref matches an exclusion"}}}
	i := &model.TriggerIntent{ID: "intent", Version: 1, LogicalKey: "key", DeliveryID: d.ID, RunID: "run", Rule: *rule, Status: model.TriggerPending, Reason: "synthetic-secret-canary", CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
	if _, _, err := s.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err != nil {
		t.Fatal(err)
	}
	if err := s.TransitionIntent(ctx, "intent", 1, model.TriggerAwaitingReview, "source ref cannot be verified; retry after reviewing provider access", nil); err != nil {
		t.Fatal(err)
	}
	p, err := s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{AppID: "app", Status: "review_required", Limit: 1})
	if err != nil || p.Total != 1 || p.Items[0].ReasonCode != "source_unavailable" {
		t.Fatal(p, err)
	}
	p.Items[0].Repository = "mutated"
	p2, err := s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{Provider: "gitea", Status: "ignored"})
	if err != nil || p2.Total != 1 || p2.Items[0].TargetSource != "decision" || p2.Items[0].Repository != "team/repo" {
		t.Fatal(p2, err)
	}
	saved, _ := s.GetDelivery(ctx, d.ID)
	if saved.Decisions[0].Status != "pending" {
		t.Fatal("projection mutated the inbox")
	}
	if q, err := s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{Search: "synthetic-secret-canary"}); err != nil || q.Total != 0 {
		t.Fatal(q, err)
	}
}
