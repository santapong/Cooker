package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func (t *TriggerStore) BeginWebhookAttempt(ctx context.Context, a *model.WebhookAttempt) error {
	if err := store.ValidateWebhookAttempt(a); err != nil {
		return err
	}
	err := t.db.QueryRowContext(ctx, `INSERT INTO webhook_attempts(id,provider,connection_id,provider_delivery_id) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING RETURNING created_at`, a.ID, a.Provider, a.ConnectionID, a.ProviderDeliveryID).Scan(&a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrConflict
	}
	return err
}
func (t *TriggerStore) FinishWebhookAttempt(ctx context.Context, id, status, code, delivery string) error {
	if err := store.ValidateWebhookAttemptOutcome(status, code); err != nil {
		return err
	}
	result, err := t.db.ExecContext(ctx, `UPDATE webhook_attempts SET status=$2,reason_code=$3,delivery_id=NULLIF($4,''),finished_at=NOW() WHERE id=$1 AND status='received'`, id, status, code, delivery)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return store.ErrConflict
	}
	return nil
}

const webhookAttemptFilter = ` FROM webhook_attempts a WHERE ($1='' OR EXISTS(SELECT 1 FROM trigger_delivery_decisions d WHERE d.delivery_id=a.delivery_id AND d.app_id=$1))
 AND ($2='' OR a.provider=$2) AND ($3='' OR a.connection_id=$3) AND ($4='' OR a.status=$4) AND ($5='' OR a.delivery_id=$5)
 AND ($6='' OR strpos(lower(concat_ws(' ',a.id,a.provider,a.connection_id,a.provider_delivery_id,a.delivery_id)),lower($6))>0)`

func (t *TriggerStore) ListWebhookAttempts(ctx context.Context, query store.WebhookAttemptQuery) (*model.WebhookAttemptPage, error) {
	q, err := query.Normalize()
	if err != nil {
		return nil, err
	}
	tx, err := t.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var cutover time.Time
	if err = tx.QueryRowContext(ctx, `SELECT started_at FROM trigger_audit_checkpoint WHERE id='v1'`).Scan(&cutover); err != nil {
		return nil, err
	}
	page := store.NewWebhookAttemptPage(q, cutover)
	args := []any{q.AppID, q.Provider, q.ConnectionID, q.Status, q.DeliveryID, q.Search}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*)`+webhookAttemptFilter, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.provider,a.connection_id,a.provider_delivery_id,COALESCE(a.delivery_id,''),a.status,a.reason_code,a.created_at,a.finished_at`+webhookAttemptFilter+` ORDER BY a.created_at DESC,a.id LIMIT $7 OFFSET $8`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a model.WebhookAttempt
		if err = rows.Scan(&a.ID, &a.Provider, &a.ConnectionID, &a.ProviderDeliveryID, &a.DeliveryID, &a.Status, &a.ReasonCode, &a.CreatedAt, &a.FinishedAt); err != nil {
			return nil, err
		}
		page.Items = append(page.Items, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return page, nil
}

var _ store.WebhookAttemptStore = (*TriggerStore)(nil)
