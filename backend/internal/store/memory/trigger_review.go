package memory

import (
	"context"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"time"
)

func (t *TriggerStore) ApproveReviewedIntent(_ context.Context, id string, version int, review *model.TriggerReview) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	i, ok := t.intents[id]
	if !ok {
		return store.ErrNotFound
	}
	if i.Version != version {
		return store.ErrConflict
	}
	if err := store.ValidateTriggerReview(i, review); err != nil {
		return err
	}
	i.Review = store.CloneTrigger(review)
	i.Status = model.TriggerPending
	i.Reason = "approved; awaiting final source validation"
	i.Version++
	i.UpdatedAt = time.Now().UTC()
	return nil
}
