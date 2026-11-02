package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestWebhookJournalMemoryImmutableDecisionsAndAttemptClones(t *testing.T) {
	ctx := context.Background()
	s := NewTriggerStore()
	rule := &model.TriggerRule{ID: "rule", AppID: "original", EnvironmentID: "environment", ActorSubject: "synthetic-secret-canary"}
	if err := s.PutRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	d := &model.TriggerDelivery{ID: "d", Event: model.VerifiedGitEvent{Source: model.GitSourceIdentity{Provider: "github", ConnectionID: "c"}, DeliveryID: "provider"}, CreatedAt: time.Now(), Decisions: []model.TriggerDecision{{RuleID: rule.ID, RuleVersion: rule.Version, AppID: "forged", Status: "ignored", Reason: "ref does not match"}}}
	if _, _, err := s.RecordDelivery(ctx, d, nil); err != nil {
		t.Fatal(err)
	}
	rule.AppID = "changed"
	rule.EnvironmentID = "changed"
	if err := s.PutRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneTriggers(ctx, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	p, err := s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{AppID: "original"})
	if err != nil || p.Total != 1 || p.Items[0].EnvironmentID != "environment" || p.Items[0].TargetID != "original" || p.Items[0].ReasonCode != "ref_mismatch" || p.Items[0].Status != "ignored" || !p.Items[0].Archived {
		t.Fatal(p, err)
	}
	a := &model.WebhookAttempt{ID: "attempt", Provider: "github", ConnectionID: "c", ProviderDeliveryID: "provider", Status: "received", ReasonCode: "processing"}
	if err := s.BeginWebhookAttempt(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishWebhookAttempt(ctx, a.ID, "duplicate", "already_recorded", "d"); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListWebhookAttempts(ctx, store.WebhookAttemptQuery{AppID: "original"})
	if err != nil || first.Total != 1 {
		t.Fatal(first, err)
	}
	*first.Items[0].FinishedAt = time.Time{}
	first.Items[0].ReasonCode = "poison"
	second, err := s.ListWebhookAttempts(ctx, store.WebhookAttemptQuery{})
	if err != nil || second.Items[0].ReasonCode != "already_recorded" || second.Items[0].FinishedAt.IsZero() {
		t.Fatal("attempt result mutates persisted audit", second, err)
	}
	if err := s.FinishWebhookAttempt(ctx, a.ID, "accepted", "admitted", "d"); !errors.Is(err, store.ErrConflict) {
		t.Fatal("terminal audit changed", err)
	}
}
