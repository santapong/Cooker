package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func triggerFixture() (*model.TriggerRule, model.VerifiedGitEvent) {
	source := model.GitSourceIdentity{Provider: "github", ConnectionID: "connection", Host: "github.com", RepositoryID: "123", Repository: "owner/repo"}
	rule := &model.TriggerRule{ID: "rule", Name: "main", Source: source, AppID: "app", AppVersion: 1, EnvironmentID: "staging", EnvironmentVersion: 2, Enabled: true, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "automatic", ActorSubject: "machine:cooker", ApprovedEnvelopeHash: strings.Repeat("d", 64)}
	event := model.VerifiedGitEvent{Source: source, DeliveryID: "delivery", Event: "push", FullRef: "refs/heads/main", BeforeSHA: strings.Repeat("a", 40), CommitSHA: strings.Repeat("b", 40), PayloadHash: strings.Repeat("c", 64)}
	return rule, event
}

func TestTriggerPatternsAndSourceBoundaries(t *testing.T) {
	rule, event := triggerFixture()
	rule.Match = "glob"
	rule.Include = []string{"release/*"}
	rule.Exclude = []string{"release/test*"}
	for _, tc := range []struct {
		ref  string
		want bool
	}{{"refs/heads/release/v1", true}, {"refs/heads/release/test1", false}, {"refs/heads/release/nested/v1", false}, {"refs/tags/release/v1", false}} {
		event.FullRef = tc.ref
		if got, _ := TriggerRuleMatches(rule, event); got != tc.want {
			t.Fatalf("%s match=%v", tc.ref, got)
		}
	}
	event.FullRef = "refs/heads/release/v1"
	event.Source.Host = "another.example"
	if got, _ := TriggerRuleMatches(rule, event); got {
		t.Fatal("matched a different Git host")
	}
	rule.Include = []string{"release/**"}
	if err := ValidateTriggerRule(rule); err == nil {
		t.Fatal("ambiguous globstar accepted")
	}
}

func TestTriggerInboxConcurrentDeliveryAndLogicalDedupe(t *testing.T) {
	ctx := context.Background()
	st := memory.NewTriggerStore()
	svc := &TriggerService{Store: st}
	rule, event := triggerFixture()
	if err := svc.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	type result struct {
		delivery  *model.TriggerDelivery
		duplicate bool
		err       error
	}
	results := make(chan result, 32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); d, dup, err := svc.AcceptVerified(ctx, event); results <- result{d, dup, err} }()
	}
	wg.Wait()
	close(results)
	newDeliveries := 0
	var first *model.TriggerDelivery
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !result.duplicate {
			newDeliveries++
		}
		first = result.delivery
	}
	if newDeliveries != 1 {
		t.Fatalf("new deliveries=%d", newDeliveries)
	}
	intents, err := st.PendingIntents(ctx, 100)
	if err != nil || len(intents) != 1 {
		t.Fatalf("intents=%d err=%v", len(intents), err)
	}
	i := intents[0]
	if i.RunID != uuid.NewSHA1(uuid.NameSpaceOID, []byte("cooker-intent:"+i.ID)).String() {
		t.Fatal("queue run identity disagrees")
	}
	event.DeliveryID = "different-delivery"
	d, dup, err := svc.AcceptVerified(ctx, event)
	if err != nil || dup || d.Decisions[0].Status != "duplicate" || d.Decisions[0].IntentID != i.ID {
		t.Fatalf("logical dedupe: %+v %v %v", d, dup, err)
	}
	event.DeliveryID = "delivery"
	event.PayloadHash = strings.Repeat("f", 64)
	if _, _, err := svc.AcceptVerified(ctx, event); !errors.Is(err, store.ErrDeliveryConflict) {
		t.Fatalf("conflicting delivery accepted: %v", err)
	}
	if first.Decisions[0].Status != "pending" {
		t.Fatalf("decision %+v", first.Decisions)
	}
}

func TestTriggerManyAppsAndGenerationChange(t *testing.T) {
	ctx := context.Background()
	st := memory.NewTriggerStore()
	svc := &TriggerService{Store: st}
	rule, event := triggerFixture()
	for i := 0; i < 3; i++ {
		cp := store.CloneTrigger(rule)
		cp.ID = fmt.Sprintf("rule-%d", i)
		cp.AppID = fmt.Sprintf("app-%d", i)
		if err := svc.SaveRule(ctx, cp); err != nil {
			t.Fatal(err)
		}
	}
	d, _, err := svc.AcceptVerified(ctx, event)
	if err != nil || len(d.Decisions) != 3 {
		t.Fatalf("many apps %+v %v", d, err)
	}
	intents, _ := st.PendingIntents(ctx, 100)
	if len(intents) != 3 {
		t.Fatalf("wanted 3 intents, got %d", len(intents))
	}
	changed, _ := st.GetRule(ctx, "rule-0")
	changed.AppVersion++
	if err := svc.SaveRule(ctx, changed); err != nil {
		t.Fatal(err)
	}
	event.DeliveryID = "after-config-change"
	if _, _, err := svc.AcceptVerified(ctx, event); err != nil {
		t.Fatal(err)
	}
	intents, _ = st.PendingIntents(ctx, 100)
	if len(intents) != 4 {
		t.Fatalf("new configuration did not create distinct intent: %d", len(intents))
	}
}

func TestTriggerMovedTagAndDefaultReview(t *testing.T) {
	ctx := context.Background()
	st := memory.NewTriggerStore()
	svc := &TriggerService{Store: st}
	rule, event := triggerFixture()
	rule.RefKind = "tag"
	rule.Match = "glob"
	rule.Include = []string{"v*"}
	event.FullRef = "refs/tags/v1"
	event.ObjectSHA = strings.Repeat("1", 40)
	if err := svc.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AcceptVerified(ctx, event); err != nil {
		t.Fatal(err)
	}
	event.DeliveryID = "moved-tag"
	event.ObjectSHA = strings.Repeat("2", 40)
	event.CommitSHA = strings.Repeat("3", 40)
	event.PayloadHash = strings.Repeat("4", 64)
	d, _, err := svc.AcceptVerified(ctx, event)
	if err != nil || d.Decisions[0].Status != "awaiting_review" {
		t.Fatalf("moved tag auto-deployed: %+v %v", d, err)
	}
	event.DeliveryID = "deleted-tag"
	event.Deleted = true
	d, _, err = svc.AcceptVerified(ctx, event)
	if err != nil || d.Decisions[0].Status != "ignored" {
		t.Fatalf("deleted tag: %+v %v", d, err)
	}
	rule.ID = "without-review"
	rule.Version = 0
	rule.ApprovedEnvelopeHash = ""
	rule.ActorSubject = ""
	if err := svc.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	event.Deleted = false
	event.FullRef = "refs/tags/v2"
	event.DeliveryID = "unapproved"
	d, _, err = svc.AcceptVerified(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range d.Decisions {
		if decision.RuleID == rule.ID && decision.Status != "awaiting_review" {
			t.Fatalf("unapproved automation accepted: %+v", decision)
		}
	}
}

func TestTriggerTagMoveRequiresReviewWithoutObservationHistory(t *testing.T) {
	ctx := context.Background()
	st := memory.NewTriggerStore()
	svc := &TriggerService{Store: st}
	rule, event := triggerFixture()
	rule.RefKind = "tag"
	rule.Include = []string{"v1"}
	event.FullRef = "refs/tags/v1"
	event.ObjectSHA = event.CommitSHA
	if err := svc.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	d, _, err := svc.AcceptVerified(ctx, event)
	if err != nil || d.Decisions[0].Status != "awaiting_review" {
		t.Fatalf("signed moved tag deployed without local history: %+v %v", d, err)
	}
}

func TestTriggerReceiptCASAndArchivePreserveDedupe(t *testing.T) {
	ctx := context.Background()
	st := memory.NewTriggerStore()
	svc := &TriggerService{Store: st}
	rule, event := triggerFixture()
	if err := svc.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	d, _, err := svc.AcceptVerified(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	i, _ := st.GetIntent(ctx, d.Decisions[0].IntentID)
	if err := st.TransitionIntent(ctx, i.ID, i.Version, model.TriggerQueued, "", &model.TriggerReceipt{RunID: "different"}); err == nil {
		t.Fatal("unrelated queue receipt accepted")
	}
	receipt := &model.TriggerReceipt{RunID: i.RunID, JobID: "job", PipelineID: "pipeline", PlanDigest: "digest"}
	if err := st.TransitionIntent(ctx, i.ID, i.Version, model.TriggerQueued, "accepted", receipt); err != nil {
		t.Fatal(err)
	}
	if err := st.TransitionIntent(ctx, i.ID, i.Version, model.TriggerFailedPreparation, "late failure", nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale transition %v", err)
	}
	if count, err := st.PruneTriggers(ctx, time.Now().Add(time.Second)); err != nil || count != 1 {
		t.Fatalf("archive %d %v", count, err)
	}
	old, dup, err := svc.AcceptVerified(ctx, event)
	if err != nil || !dup || !old.Archived {
		t.Fatalf("old authenticated delivery lost dedupe: %+v %v %v", old, dup, err)
	}
	event.DeliveryID = "after-history-compaction"
	next, _, err := svc.AcceptVerified(ctx, event)
	if err != nil || next.Decisions[0].Status != "duplicate" {
		t.Fatalf("logical tombstone lost: %+v %v", next, err)
	}
}
