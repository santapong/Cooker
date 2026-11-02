package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func (t *TriggerStore) ApproveReviewedIntent(ctx context.Context, id string, version int, review *model.TriggerReview) error {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	i, err := scanTriggerIntent(tx.QueryRowContext(ctx, `SELECT `+triggerIntentColumns+` FROM trigger_intents WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	if i.Version != version {
		return store.ErrConflict
	}
	if err = store.ValidateTriggerReview(i, review); err != nil {
		return err
	}
	b, err := json.Marshal(review)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE trigger_intents SET spec=jsonb_set(spec,'{review}',$2::jsonb),status='pending',reason='approved; awaiting final source validation',version=version+1,updated_at=NOW() WHERE id=$1`, id, b)
	if err != nil {
		return err
	}
	return tx.Commit()
}
