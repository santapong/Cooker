package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type TriggerStore struct {
	mu              sync.Mutex
	rules           map[string]*model.TriggerRule
	deliveries      map[string]*model.TriggerDelivery
	deliveryKeys    map[string]string
	intents         map[string]*model.TriggerIntent
	logicalKeys     map[string]string
	tags            map[string][2]string
	decisionJournal map[string][]model.TriggerDecisionRecord
	attempts        map[string]*model.WebhookAttempt
	auditStartedAt  time.Time
	nativeAdmission chan struct{}
}

func NewTriggerStore() *TriggerStore {
	return &TriggerStore{rules: map[string]*model.TriggerRule{}, deliveries: map[string]*model.TriggerDelivery{}, deliveryKeys: map[string]string{}, intents: map[string]*model.TriggerIntent{}, logicalKeys: map[string]string{}, tags: map[string][2]string{}, decisionJournal: map[string][]model.TriggerDecisionRecord{}, attempts: map[string]*model.WebhookAttempt{}, auditStartedAt: time.Now().UTC(), nativeAdmission: make(chan struct{}, 1)}
}

func (t *TriggerStore) PutRule(_ context.Context, r *model.TriggerRule) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	old, exists := t.rules[r.ID]
	if (exists && old.Version != r.Version) || (!exists && r.Version != 0) {
		return store.ErrConflict
	}
	cp := store.CloneTrigger(r)
	cp.Version++
	cp.UpdatedAt = time.Now().UTC()
	if exists {
		cp.CreatedAt = old.CreatedAt
	} else {
		cp.CreatedAt = cp.UpdatedAt
	}
	t.rules[r.ID] = cp
	*r = *store.CloneTrigger(cp)
	return nil
}
func (t *TriggerStore) GetRule(_ context.Context, id string) (*model.TriggerRule, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.rules[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return store.CloneTrigger(r), nil
}
func (t *TriggerStore) ListRules(_ context.Context, connection string) ([]*model.TriggerRule, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []*model.TriggerRule
	for _, r := range t.rules {
		if connection == "" || r.Source.ConnectionID == connection {
			out = append(out, store.CloneTrigger(r))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (t *TriggerStore) RecordDelivery(_ context.Context, d *model.TriggerDelivery, intents []*model.TriggerIntent) (*model.TriggerDelivery, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := d.Event.Source.Provider + "\x00" + d.Event.Source.ConnectionID + "\x00" + d.Event.DeliveryID
	if id, ok := t.deliveryKeys[key]; ok {
		old := t.deliveries[id]
		if !store.SameTriggerDelivery(old, d) {
			return nil, false, store.ErrDeliveryConflict
		}
		return store.CloneTrigger(old), true, nil
	}
	for _, decision := range d.Decisions {
		if decision.RuleID == "" {
			continue
		}
		rule, ok := t.rules[decision.RuleID]
		if !ok || rule.Version != decision.RuleVersion {
			return nil, false, store.ErrConflict
		}
	}
	copy := store.CloneTrigger(d)
	moved := store.TriggerTagMovedByEvent(d.Event)
	if d.Event.Event == "push" && !d.Event.Deleted && strings.HasPrefix(d.Event.FullRef, "refs/tags/") {
		tagKey := strings.Join([]string{d.Event.Source.Provider, d.Event.Source.ConnectionID, d.Event.Source.Host, d.Event.Source.RepositoryID, d.Event.FullRef}, "\x00")
		observed := [2]string{store.TriggerTagObject(d.Event), d.Event.CommitSHA}
		previous, exists := t.tags[tagKey]
		moved = moved || (exists && previous != observed)
		t.tags[tagKey] = observed
	}
	for _, intent := range intents {
		if id, exists := t.logicalKeys[intent.LogicalKey]; exists {
			store.ApplyTriggerIntentDecision(copy, t.intents[id], true, intent.ID)
			continue
		}
		cp := store.CloneTrigger(intent)
		if moved {
			cp.Status = model.TriggerAwaitingReview
			cp.Reason = "release tag moved; verify the new revision before deployment"
		}
		t.intents[cp.ID] = cp
		t.logicalKeys[cp.LogicalKey] = cp.ID
		store.ApplyTriggerIntentDecision(copy, cp, false)
	}
	for index := range copy.Decisions {
		decision := &copy.Decisions[index]
		// Resolve target from the exact locked rule generation, never a caller's
		// claim or a later current rule. These minimal rows survive compaction.
		decision.AppID = ""
		decision.EnvironmentID = ""
		decision.TargetKind = ""
		decision.TargetID = ""
		if r, ok := t.rules[decision.RuleID]; ok {
			decision.AppID = r.AppID
			decision.EnvironmentID = r.EnvironmentID
			decision.TargetKind, decision.TargetID = r.Target()
		}
		t.decisionJournal[copy.ID] = append(t.decisionJournal[copy.ID], store.MinimalTriggerDecision(copy.ID, index+1, *decision))
	}
	t.deliveries[copy.ID] = copy
	t.deliveryKeys[key] = copy.ID
	return store.CloneTrigger(copy), false, nil
}
func (t *TriggerStore) GetDelivery(_ context.Context, id string) (*model.TriggerDelivery, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	d, ok := t.deliveries[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return store.CloneTrigger(d), nil
}
func (t *TriggerStore) GetIntent(_ context.Context, id string) (*model.TriggerIntent, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i, ok := t.intents[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return store.CloneTrigger(i), nil
}
func (t *TriggerStore) PendingIntents(_ context.Context, limit int) ([]*model.TriggerIntent, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var out []*model.TriggerIntent
	for _, i := range t.intents {
		if i.Status == model.TriggerPending {
			out = append(out, store.CloneTrigger(i))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (t *TriggerStore) ListIntents(_ context.Context, appID string, limit int) ([]*model.TriggerIntent, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	out := []*model.TriggerIntent{}
	for _, intent := range t.intents {
		if appID == "" || intent.Rule.AppID == appID {
			out = append(out, store.CloneTrigger(intent))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (t *TriggerStore) TransitionIntent(_ context.Context, id string, version int, status model.TriggerIntentStatus, reason string, receipt *model.TriggerReceipt) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	i, ok := t.intents[id]
	if !ok {
		return store.ErrNotFound
	}
	if i.Version != version {
		return store.ErrConflict
	}
	if err := store.ValidateTriggerTransition(i, status, reason, receipt); err != nil {
		return err
	}
	i.Version++
	i.Status = status
	i.Reason = reason
	i.Receipt = store.CloneTrigger(receipt)
	i.UpdatedAt = time.Now().UTC()
	return nil
}

func (t *TriggerStore) PruneTriggers(_ context.Context, before time.Time) (int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var count int64
	for _, d := range t.deliveries {
		if d.Archived || !d.CreatedAt.Before(before) {
			continue
		}
		keep := false
		for _, i := range t.intents {
			if i.DeliveryID == d.ID && (i.Status == model.TriggerPending || i.Status == model.TriggerAwaitingReview) {
				keep = true
			}
		}
		if keep {
			continue
		}
		for _, i := range t.intents {
			if i.DeliveryID == d.ID {
				store.CompactTriggerIntent(i)
			}
		}
		d.Decisions = nil
		d.Archived = true
		count++
	}
	return count, nil
}

var _ store.TriggerStore = (*TriggerStore)(nil)
