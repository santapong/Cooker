package memory

import (
	"context"
	"sort"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func (t *TriggerStore) BeginWebhookAttempt(_ context.Context, a *model.WebhookAttempt) error {
	if err := store.ValidateWebhookAttempt(a); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.attempts[a.ID]; ok {
		return store.ErrConflict
	}
	copy := store.CloneTrigger(a)
	copy.CreatedAt = time.Now().UTC()
	t.attempts[a.ID] = copy
	*a = *store.CloneTrigger(copy)
	return nil
}
func (t *TriggerStore) FinishWebhookAttempt(_ context.Context, id, status, code, delivery string) error {
	if err := store.ValidateWebhookAttemptOutcome(status, code); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	a, ok := t.attempts[id]
	if !ok {
		return store.ErrNotFound
	}
	if a.Status != "received" {
		return store.ErrConflict
	}
	if delivery != "" {
		if _, ok := t.deliveries[delivery]; !ok {
			return store.ErrNotFound
		}
	}
	now := time.Now().UTC()
	a.Status = status
	a.ReasonCode = code
	a.DeliveryID = delivery
	a.FinishedAt = &now
	return nil
}
func (t *TriggerStore) ListWebhookAttempts(_ context.Context, query store.WebhookAttemptQuery) (*model.WebhookAttemptPage, error) {
	q, err := query.Normalize()
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	page := store.NewWebhookAttemptPage(q, t.auditStartedAt)
	for _, a := range t.attempts {
		if !store.WebhookAttemptMatches(*a, q) {
			continue
		}
		if q.AppID != "" {
			matched := false
			for _, d := range t.decisionJournal[a.DeliveryID] {
				if d.AppID == q.AppID {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		page.Items = append(page.Items, *store.CloneTrigger(a))
	}
	sort.Slice(page.Items, func(i, j int) bool {
		if !page.Items[i].CreatedAt.Equal(page.Items[j].CreatedAt) {
			return page.Items[i].CreatedAt.After(page.Items[j].CreatedAt)
		}
		return page.Items[i].ID < page.Items[j].ID
	})
	page.Total = len(page.Items)
	if q.Offset >= page.Total {
		page.Items = []model.WebhookAttempt{}
		return page, nil
	}
	end := q.Offset + q.Limit
	if end > page.Total {
		end = page.Total
	}
	page.Items = page.Items[q.Offset:end]
	return page, nil
}

var _ store.WebhookAttemptStore = (*TriggerStore)(nil)
