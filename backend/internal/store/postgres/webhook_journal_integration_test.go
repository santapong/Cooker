//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestWebhookJournalPostgresImmutableTargetsRetentionAndLegacyCutover(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	s := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, s)
	rule, _ := s.GetRule(ctx, i.Rule.ID)
	rule.EnvironmentID = "original-environment"
	rule.ActorSubject = "synthetic-secret-canary"
	rule.PlanHash = "synthetic-secret-canary"
	if err := s.PutRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	d.Decisions = []model.TriggerDecision{{RuleID: rule.ID, RuleVersion: rule.Version, AppID: "forged-target", EnvironmentID: "forged-environment", Status: "ignored", Reason: "ref matches an exclusion"}}
	if _, _, err := s.RecordDelivery(ctx, d, nil); err != nil {
		t.Fatal(err)
	}
	legacy := store.CloneTrigger(d)
	legacy.ID = "legacy"
	legacy.Event.DeliveryID = "legacy-provider"
	if _, _, err := s.RecordDelivery(ctx, legacy, nil); err != nil {
		t.Fatal(err)
	}
	// Simulate a genuine older retained delivery without retrospectively
	// inventing the immutable target which old schemas did not record.
	if _, err := db.Exec(`DELETE FROM trigger_delivery_decisions WHERE delivery_id='legacy'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE webhook_deliveries SET decisions='[{"ruleId":"rule","ruleVersion":2,"status":"ignored","reason":"ref matches an exclusion"}]' WHERE id='legacy'`); err != nil {
		t.Fatal(err)
	}
	rule.AppID = "new-app"
	rule.EnvironmentID = "new-environment"
	if err := s.PutRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	p, err := s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{AppID: "app", Status: "ignored"})
	if err != nil || p.Total != 1 || p.Items[0].AppID != "app" || p.Items[0].EnvironmentID != "original-environment" || p.Items[0].TargetSource != "decision" || p.Items[0].RuleVersion != 2 || p.JournalStartedAt == nil {
		t.Fatal(p, err)
	}
	p, err = s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{AppID: "new-app"})
	if err != nil || p.Total != 1 || p.Items[0].DeliveryID != "legacy" || p.Items[0].TargetSource != "current_rule" || p.Items[0].MetadataCoverage != "legacy" {
		t.Fatal(p, err)
	}
	if _, err = s.PruneTriggers(ctx, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	p, err = NewTriggerStore(db).ListTriggerHistory(ctx, store.TriggerHistoryQuery{AppID: "app", Status: "ignored"})
	if err != nil || p.Total != 1 || !p.Items[0].Archived || p.Items[0].ReasonCode != "ref_excluded" {
		t.Fatal(p, err)
	}
	p, err = s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{DeliveryID: "legacy"})
	if err != nil || p.Total != 1 || p.Items[0].Status != "archived" || p.Items[0].AppID != "" {
		t.Fatal(p, err)
	}
	var records []byte
	if err = db.QueryRow(`SELECT jsonb_agg(to_jsonb(d)) FROM trigger_delivery_decisions d`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(records), "synthetic-secret-canary") || strings.Contains(string(records), "forged-target") {
		t.Fatal("private or caller authority copied into minimal journal")
	}
}

func TestWebhookJournalPostgresAttemptPaginationRestartAndAllowedOutcomes(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	s := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, s)
	if _, _, err := s.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 140; n++ {
		a := &model.WebhookAttempt{ID: fmt.Sprintf("attempt-%03d", n), Provider: "github", ConnectionID: "c", ProviderDeliveryID: fmt.Sprintf("provider-%03d", n), Status: "received", ReasonCode: "processing"}
		if err := s.BeginWebhookAttempt(ctx, a); err != nil {
			t.Fatal(err)
		}
		if n < 110 {
			if err := s.FinishWebhookAttempt(ctx, a.ID, "accepted", "admitted", d.ID); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := s.FinishWebhookAttempt(ctx, a.ID, "rejected", "invalid_json", ""); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(`UPDATE webhook_attempts SET created_at=$2 WHERE id=$1`, a.ID, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(n)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	reopened := NewTriggerStore(db)
	p, err := reopened.ListWebhookAttempts(ctx, store.WebhookAttemptQuery{Status: "rejected", Provider: "github", Limit: 7, Offset: 21})
	if err != nil || p.Total != 30 || len(p.Items) != 7 || p.Items[0].ID != "attempt-131" || p.JournalStartedAt.IsZero() {
		t.Fatal(p, err)
	}
	p, err = reopened.ListWebhookAttempts(ctx, store.WebhookAttemptQuery{AppID: "app", Status: "accepted", Limit: 1})
	if err != nil || p.Total != 110 {
		t.Fatal(p, err)
	}
	for _, q := range []store.WebhookAttemptQuery{{AppID: "other-app"}, {Search: "synthetic-secret-canary"}, {Search: "%_"}, {Provider: "gitlab"}} {
		p, err := reopened.ListWebhookAttempts(ctx, q)
		if err != nil || p.Total != 0 {
			t.Fatal(p, err)
		}
	}
	crashed := &model.WebhookAttempt{ID: "interrupted", Provider: "gitea", ConnectionID: "gitea", Status: "received", ReasonCode: "processing"}
	if err := s.BeginWebhookAttempt(ctx, crashed); err != nil {
		t.Fatal(err)
	}
	p, err = reopened.ListWebhookAttempts(ctx, store.WebhookAttemptQuery{Status: "received"})
	if err != nil || p.Total != 1 || p.Items[0].FinishedAt != nil {
		t.Fatal(p, err)
	}
	if err := s.BeginWebhookAttempt(ctx, crashed); !errors.Is(err, store.ErrConflict) {
		t.Fatal("attempt identity reused", err)
	}
	if err := s.FinishWebhookAttempt(ctx, "interrupted", "uncertain", "admission_unconfirmed", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishWebhookAttempt(ctx, "interrupted", "accepted", "admitted", d.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatal("terminal audit rewritten", err)
	}
	if err := s.FinishWebhookAttempt(ctx, "interrupted", "failed", "synthetic-secret-canary", ""); err == nil {
		t.Fatal("private reason accepted")
	}
	if _, err := db.Exec(`INSERT INTO webhook_attempts(id,provider,connection_id,status,reason_code) VALUES('poison','github','c','received','synthetic-secret-canary')`); err == nil {
		t.Fatal("database admitted private reason")
	}
	p, err = reopened.ListWebhookAttempts(ctx, store.WebhookAttemptQuery{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "synthetic-secret-canary") || strings.Contains(string(b), "rawBody") || strings.Contains(string(b), "payloadHash") {
		t.Fatal("audit escaped allowlist")
	}
}
