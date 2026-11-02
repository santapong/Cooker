//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestTriggerHistoryPostgresFiltersBeforePaginationAndSafePersistence(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	s := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, s)
	other := store.CloneTrigger(&i.Rule)
	other.ID = "other-rule"
	other.Version = 0
	other.AppID = "other-app"
	if err := s.PutRule(ctx, other); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 140; n++ {
		delivery := store.CloneTrigger(d)
		intent := store.CloneTrigger(i)
		delivery.ID = fmt.Sprintf("d-%03d", n)
		delivery.Event.DeliveryID = delivery.ID
		delivery.CreatedAt = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(n) * time.Minute)
		intent.ID = fmt.Sprintf("i-%03d", n)
		intent.LogicalKey = intent.ID
		intent.DeliveryID = delivery.ID
		intent.Event = delivery.Event
		intent.CreatedAt = delivery.CreatedAt
		intent.UpdatedAt = delivery.CreatedAt
		intent.Reason = "synthetic-secret-canary"
		if n < 110 {
			intent.Rule = *other
			delivery.Decisions[0].RuleID = other.ID
			delivery.Decisions[0].RuleVersion = other.Version
		}
		delivery.Decisions[0].IntentID = intent.ID
		if _, _, err := s.RecordDelivery(ctx, delivery, []*model.TriggerIntent{intent}); err != nil {
			t.Fatal(err)
		}
	}
	// Poison private/unknown JSON fields through actual JSONB persistence. The
	// allowlisted read must neither serialize them nor search their contents.
	if _, err := db.Exec(`UPDATE webhook_deliveries SET event=event||'{"rawBody":"synthetic-secret-canary","authorization":"synthetic-secret-canary"}'::jsonb`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE trigger_intents SET spec=spec||'{"environment":{"PASSWORD":"synthetic-secret-canary"}}'::jsonb`); err != nil {
		t.Fatal(err)
	}
	reopened := NewTriggerStore(db)
	page, err := reopened.ListTriggerHistory(ctx, store.TriggerHistoryQuery{AppID: "app", Status: "accepted", Provider: "github", Ref: "refs/heads/main", Limit: 7, Offset: 21})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 30 || len(page.Items) != 7 || page.Items[0].DeliveryID != "d-131" || page.Items[6].DeliveryID != "d-137" {
		t.Fatalf("filters/pagination %+v", page)
	}
	b, _ := json.Marshal(page)
	if strings.Contains(string(b), "synthetic-secret-canary") || strings.Contains(string(b), "payloadHash") || strings.Contains(string(b), "environment") {
		t.Fatal("private data escaped allowlist")
	}
	for _, query := range []store.TriggerHistoryQuery{{Search: "synthetic-secret-canary"}, {Search: "%_"}, {Provider: "gitlab"}, {ConnectionID: "other-connection"}, {AppID: "missing"}} {
		p, err := reopened.ListTriggerHistory(ctx, query)
		if err != nil || p.Total != 0 || len(p.Items) != 0 {
			t.Fatalf("bad empty filter: %+v %v", p, err)
		}
	}
	last, err := reopened.ListTriggerHistory(ctx, store.TriggerHistoryQuery{AppID: "app", Limit: 7, Offset: 999})
	if err != nil || last.Total != 30 || len(last.Items) != 0 {
		t.Fatal(last, err)
	}
	// The current lifecycle is joined before filters, never just the original
	// pending decision stored in the inbox.
	if err := reopened.TransitionIntent(ctx, "i-139", 1, model.TriggerAwaitingReview, "source ref cannot be verified; retry after reviewing provider access", nil); err != nil {
		t.Fatal(err)
	}
	p, err := reopened.ListTriggerHistory(ctx, store.TriggerHistoryQuery{AppID: "app", Status: "review_required", Limit: 1})
	if err != nil || p.Total != 1 || p.Items[0].ReasonCode != "source_unavailable" {
		t.Fatal(p, err)
	}
}

func TestTriggerHistoryPostgresDuplicateReceiptsFailuresAndCompaction(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	s := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, s)
	if _, _, err := s.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err != nil {
		t.Fatal(err)
	}
	second := store.CloneTrigger(d)
	second.ID = "duplicate-delivery"
	second.Event.DeliveryID = "duplicate-provider-delivery"
	second.Decisions[0].IntentID = "proposed-duplicate"
	proposal := store.CloneTrigger(i)
	proposal.ID = "proposed-duplicate"
	proposal.DeliveryID = second.ID
	if _, _, err := s.RecordDelivery(ctx, second, []*model.TriggerIntent{proposal}); err != nil {
		t.Fatal(err)
	}
	if err := NewPipelineStore(db).Create(ctx, &model.Pipeline{ID: "pipeline", Name: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := NewRunStore(db).Create(ctx, &model.PipelineRun{ID: "run", PipelineID: "pipeline", Status: model.RunStatusFailed, Error: "synthetic-secret-canary", Variables: map[string]string{"secret": "synthetic-secret-canary"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,payload,status,last_error) VALUES('job','app-run','{"token":"synthetic-secret-canary"}','succeeded','synthetic-secret-canary')`); err != nil {
		t.Fatal(err)
	}
	if err := s.TransitionIntent(ctx, i.ID, 1, model.TriggerQueued, "deployment durably queued", &model.TriggerReceipt{JobID: "job", RunID: "run", PipelineID: "pipeline", PlanDigest: "synthetic-secret-canary"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{Status: "failed", Limit: 1})
	if err != nil || p.Total != 1 || p.Items[0].DeliveryID != d.ID || p.Items[0].RunStatus != "failed" || p.Items[0].JobStatus != "succeeded" {
		t.Fatal(p, err)
	}
	p, err = s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{Status: "duplicate"})
	if err != nil || p.Total != 1 || p.Items[0].IntentID != i.ID || p.Items[0].JobID != "job" {
		t.Fatal(p, err)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "synthetic-secret-canary") {
		t.Fatal("private receipt/job/run data escaped")
	}
	// Duplicate identity/decision remains distinct from the original failed
	// deployment. Prospective minimal decisions now survive rich compaction.
	if _, err = s.PruneTriggers(ctx, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	p, err = NewTriggerStore(db).ListTriggerHistory(ctx, store.TriggerHistoryQuery{DeliveryID: d.ID})
	if err != nil || p.Total != 1 || p.Items[0].Status != "failed" || !p.Items[0].Archived || p.Items[0].JobID != "job" {
		t.Fatal(p, err)
	}
	p, err = s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{DeliveryID: second.ID})
	if err != nil || p.Total != 1 || p.Items[0].Status != "duplicate" || p.Items[0].IntentID != i.ID {
		t.Fatal(p, err)
	}
}
