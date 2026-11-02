//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func triggerRecordFixture(t *testing.T, s *TriggerStore) (*model.TriggerDelivery, *model.TriggerIntent) {
	t.Helper()
	source := model.GitSourceIdentity{Provider: "github", Host: "github.com", ConnectionID: "c", RepositoryID: "repo-id", Repository: "org/repo"}
	r := &model.TriggerRule{ID: "rule", Source: source, AppID: "app", RefKind: "branch", Match: "exact", Include: []string{"main"}, Enabled: true}
	if err := s.PutRule(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	event := model.VerifiedGitEvent{Source: source, DeliveryID: "provider-delivery", Event: "push", FullRef: "refs/heads/main", CommitSHA: strings.Repeat("b", 40), PayloadHash: strings.Repeat("a", 64)}
	now := time.Now().UTC()
	d := &model.TriggerDelivery{ID: "delivery", Event: event, CreatedAt: now, Decisions: []model.TriggerDecision{{RuleID: r.ID, RuleVersion: r.Version, Status: "pending", IntentID: "intent"}}}
	i := &model.TriggerIntent{ID: "intent", Version: 1, LogicalKey: "logical-key", DeliveryID: d.ID, RunID: "run", Rule: *r, Event: event, Status: model.TriggerPending, Reason: "ready for admission", CreatedAt: now, UpdatedAt: now}
	return d, i
}

func TestTriggerPostgresConcurrentInboxDedupeAndRestart(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	s := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, s)
	type result struct {
		duplicate bool
		err       error
	}
	results := make(chan result, 24)
	var wg sync.WaitGroup
	for n := 0; n < 24; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, dup, err := s.RecordDelivery(ctx, store.CloneTrigger(d), []*model.TriggerIntent{store.CloneTrigger(i)})
			results <- result{dup, err}
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if !r.duplicate {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("new concurrent deliveries=%d", accepted)
	}
	reopened := NewTriggerStore(db)
	pending, err := reopened.PendingIntents(ctx, 100)
	if err != nil || len(pending) != 1 {
		t.Fatalf("restart pending=%d %v", len(pending), err)
	}
	if err := reopened.TransitionIntent(ctx, pending[0].ID, pending[0].Version, model.TriggerAwaitingReview, "manual review", nil); err != nil {
		t.Fatal(err)
	}
	recent, err := reopened.ListIntents(ctx, "app", 1)
	if err != nil || len(recent) != 1 || recent[0].Status != model.TriggerAwaitingReview {
		t.Fatalf("review list: %+v %v", recent, err)
	}
	if other, err := reopened.ListIntents(ctx, "different-app", 10); err != nil || len(other) != 0 {
		t.Fatalf("cross-app list: %+v %v", other, err)
	}
	if err := reopened.TransitionIntent(ctx, recent[0].ID, recent[0].Version, model.TriggerPending, "reviewed", nil); err != nil {
		t.Fatal(err)
	}
	changed := store.CloneTrigger(d)
	changed.Event.PayloadHash = strings.Repeat("f", 64)
	if _, _, err := reopened.RecordDelivery(ctx, changed, nil); !errors.Is(err, store.ErrDeliveryConflict) {
		t.Fatalf("delivery collision accepted: %v", err)
	}
	// A new provider delivery for the same logical release must refer to the
	// original intent, even when callers supplied a different proposed ID.
	other := store.CloneTrigger(d)
	other.ID = "delivery-2"
	other.Event.DeliveryID = "provider-delivery-2"
	other.Decisions[0].IntentID = "proposal-2"
	proposal := store.CloneTrigger(i)
	proposal.ID = "proposal-2"
	proposal.DeliveryID = other.ID
	recorded, _, err := reopened.RecordDelivery(ctx, other, []*model.TriggerIntent{proposal})
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Decisions[0].Status != "duplicate" || recorded.Decisions[0].IntentID != "intent" {
		t.Fatalf("logical collision lost reference: %+v", recorded)
	}
	receipt := &model.TriggerReceipt{RunID: "run", JobID: "job", PipelineID: "p", PlanDigest: "hash"}
	if err := reopened.TransitionIntent(ctx, "intent", 3, model.TriggerQueued, "accepted", receipt); err != nil {
		t.Fatal(err)
	}
	if err := reopened.TransitionIntent(ctx, "intent", 1, model.TriggerFailedPreparation, "stale", nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale status accepted: %v", err)
	}
	if _, err := reopened.PruneTriggers(ctx, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	old, dup, err := reopened.RecordDelivery(ctx, d, []*model.TriggerIntent{i})
	if err != nil || !dup || !old.Archived {
		t.Fatalf("archive erased dedupe: %+v %v %v", old, dup, err)
	}
	saved, err := reopened.GetIntent(ctx, "intent")
	if err != nil || !saved.Archived || saved.Receipt == nil || saved.Receipt.JobID != "job" {
		t.Fatalf("archive erased receipt: %+v %v", saved, err)
	}
}

func TestTriggerPostgresAtomicRuleGenerationAndMovedTag(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	s := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, s)
	rule, _ := s.GetRule(ctx, "rule")
	rule.Name = "changed"
	if err := s.PutRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale rule generation accepted: %v", err)
	}
	if _, err := s.GetDelivery(ctx, d.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rejected generation partially saved delivery: %v", err)
	}
	d.Decisions[0].RuleVersion = rule.Version
	i.Rule = *rule
	d.Event.FullRef = "refs/tags/v1"
	d.Event.ObjectSHA = strings.Repeat("1", 40)
	i.Event = d.Event
	if _, _, err := s.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err != nil {
		t.Fatal(err)
	}
	for n := 2; n <= 3; n++ {
		moved := store.CloneTrigger(d)
		moved.ID = fmt.Sprintf("delivery-%d", n)
		moved.Event.DeliveryID = moved.ID
		moved.Event.ObjectSHA = strings.Repeat(fmt.Sprint(n), 40)
		moved.Event.CommitSHA = strings.Repeat(fmt.Sprint(n+2), 40)
		moved.Event.PayloadHash = strings.Repeat(fmt.Sprint(n), 64)
		intent := store.CloneTrigger(i)
		intent.ID = fmt.Sprintf("intent-%d", n)
		intent.LogicalKey = intent.ID
		intent.DeliveryID = moved.ID
		intent.Event = moved.Event
		moved.Decisions[0].IntentID = intent.ID
		accepted, _, err := s.RecordDelivery(ctx, moved, []*model.TriggerIntent{intent})
		if err != nil {
			t.Fatal(err)
		}
		if accepted.Decisions[0].Status != "awaiting_review" {
			t.Fatalf("moved tag bypassed review: %+v", accepted)
		}
	}
}
