package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type TriggerStore struct{ db *sql.DB }

func NewTriggerStore(db *sql.DB) *TriggerStore { return &TriggerStore{db: db} }

func (t *TriggerStore) PutRule(ctx context.Context, r *model.TriggerRule) error {
	cp := store.CloneTrigger(r)
	cp.Version++
	cp.UpdatedAt = time.Now().UTC()
	if r.Version == 0 {
		cp.CreatedAt = cp.UpdatedAt
	}
	body, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	if r.Version == 0 {
		_, err = t.db.ExecContext(ctx, `INSERT INTO trigger_rules(id,version,connection_id,body,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6)`, cp.ID, cp.Version, cp.Source.ConnectionID, body, cp.CreatedAt, cp.UpdatedAt)
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return store.ErrConflict
		}
	} else {
		err = t.db.QueryRowContext(ctx, `UPDATE trigger_rules SET version=$2,connection_id=$3,body=$4,updated_at=$5 WHERE id=$1 AND version=$6 RETURNING created_at`, cp.ID, cp.Version, cp.Source.ConnectionID, body, cp.UpdatedAt, r.Version).Scan(&cp.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrConflict
		}
	}
	if err == nil {
		*r = *cp
	}
	return err
}

func scanTriggerRule(row scannable) (*model.TriggerRule, error) {
	var r model.TriggerRule
	var body []byte
	var version int
	var created, updated time.Time
	if err := row.Scan(&body, &version, &created, &updated); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	r.Version = version
	r.CreatedAt = created
	r.UpdatedAt = updated
	return &r, nil
}
func (t *TriggerStore) GetRule(ctx context.Context, id string) (*model.TriggerRule, error) {
	r, err := scanTriggerRule(t.db.QueryRowContext(ctx, `SELECT body,version,created_at,updated_at FROM trigger_rules WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}
	return r, err
}
func (t *TriggerStore) ListRules(ctx context.Context, connection string) ([]*model.TriggerRule, error) {
	rows, err := t.db.QueryContext(ctx, `SELECT body,version,created_at,updated_at FROM trigger_rules WHERE $1='' OR connection_id=$1 ORDER BY id`, connection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.TriggerRule
	for rows.Next() {
		r, err := scanTriggerRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const triggerDeliveryColumns = `id,event,decisions,created_at,archived`

func scanTriggerDelivery(row scannable) (*model.TriggerDelivery, error) {
	d := &model.TriggerDelivery{}
	var event, decisions []byte
	if err := row.Scan(&d.ID, &event, &decisions, &d.CreatedAt, &d.Archived); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(event, &d.Event); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(decisions, &d.Decisions); err != nil {
		return nil, err
	}
	return d, nil
}
func (t *TriggerStore) GetDelivery(ctx context.Context, id string) (*model.TriggerDelivery, error) {
	d, err := scanTriggerDelivery(t.db.QueryRowContext(ctx, `SELECT `+triggerDeliveryColumns+` FROM webhook_deliveries WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}
	return d, err
}

const triggerIntentColumns = `spec,version,status,reason,receipt,created_at,updated_at,archived`

func scanTriggerIntent(row scannable) (*model.TriggerIntent, error) {
	i := &model.TriggerIntent{}
	var spec, receipt []byte
	var version int
	var status model.TriggerIntentStatus
	var reason string
	var created, updated time.Time
	var archived bool
	if err := row.Scan(&spec, &version, &status, &reason, &receipt, &created, &updated, &archived); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(spec, i); err != nil {
		return nil, err
	}
	if len(receipt) > 0 {
		if err := json.Unmarshal(receipt, &i.Receipt); err != nil {
			return nil, err
		}
	}
	i.Version = version
	i.Status = status
	i.Reason = reason
	i.CreatedAt = created
	i.UpdatedAt = updated
	i.Archived = archived
	return i, nil
}
func (t *TriggerStore) GetIntent(ctx context.Context, id string) (*model.TriggerIntent, error) {
	i, err := scanTriggerIntent(t.db.QueryRowContext(ctx, `SELECT `+triggerIntentColumns+` FROM trigger_intents WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}
	return i, err
}

func (t *TriggerStore) RecordDelivery(ctx context.Context, d *model.TriggerDelivery, intents []*model.TriggerIntent) (*model.TriggerDelivery, bool, error) {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }() // Commit/operation errors determine the result.
	event, err := json.Marshal(d.Event)
	if err != nil {
		return nil, false, err
	}
	var inserted string
	err = tx.QueryRowContext(ctx, `INSERT INTO webhook_deliveries(id,provider,connection_id,delivery_id,payload_hash,event,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(provider,connection_id,delivery_id) DO NOTHING RETURNING id`, d.ID, d.Event.Source.Provider, d.Event.Source.ConnectionID, d.Event.DeliveryID, d.Event.PayloadHash, event, d.CreatedAt).Scan(&inserted)
	if errors.Is(err, sql.ErrNoRows) {
		old, readErr := scanTriggerDelivery(tx.QueryRowContext(ctx, `SELECT `+triggerDeliveryColumns+` FROM webhook_deliveries WHERE provider=$1 AND connection_id=$2 AND delivery_id=$3`, d.Event.Source.Provider, d.Event.Source.ConnectionID, d.Event.DeliveryID))
		if readErr != nil {
			return nil, false, readErr
		}
		if !store.SameTriggerDelivery(old, d) {
			return nil, false, store.ErrDeliveryConflict
		}
		return old, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	copy := store.CloneTrigger(d)
	// Lock rule generations in stable order; edited rules cannot race admission.
	decisions := append([]model.TriggerDecision(nil), d.Decisions...)
	targets := map[string]*model.TriggerRule{}
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].RuleID < decisions[j].RuleID })
	for _, decision := range decisions {
		if decision.RuleID == "" {
			continue
		}
		rule, err := scanTriggerRule(tx.QueryRowContext(ctx, `SELECT body,version,created_at,updated_at FROM trigger_rules WHERE id=$1 FOR SHARE`, decision.RuleID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				err = store.ErrConflict
			}
			return nil, false, err
		}
		if rule.Version != decision.RuleVersion {
			return nil, false, store.ErrConflict
		}
		targets[decision.RuleID] = rule
	}
	moved := store.TriggerTagMovedByEvent(d.Event)
	if d.Event.Event == "push" && !d.Event.Deleted && strings.HasPrefix(d.Event.FullRef, "refs/tags/") {
		s := d.Event.Source
		object := store.TriggerTagObject(d.Event)
		_, err = tx.ExecContext(ctx, `INSERT INTO trigger_tag_observations(provider,host,connection_id,repository_id,full_ref,object_sha,commit_sha) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, s.Provider, s.Host, s.ConnectionID, s.RepositoryID, d.Event.FullRef, object, d.Event.CommitSHA)
		if err != nil {
			return nil, false, err
		}
		var oldObject, oldCommit string
		err = tx.QueryRowContext(ctx, `SELECT object_sha,commit_sha FROM trigger_tag_observations WHERE provider=$1 AND host=$2 AND connection_id=$3 AND repository_id=$4 AND full_ref=$5 FOR UPDATE`, s.Provider, s.Host, s.ConnectionID, s.RepositoryID, d.Event.FullRef).Scan(&oldObject, &oldCommit)
		if err != nil {
			return nil, false, err
		}
		moved = moved || oldObject != object || oldCommit != d.Event.CommitSHA
		_, err = tx.ExecContext(ctx, `UPDATE trigger_tag_observations SET object_sha=$6,commit_sha=$7,observed_at=NOW() WHERE provider=$1 AND host=$2 AND connection_id=$3 AND repository_id=$4 AND full_ref=$5`, s.Provider, s.Host, s.ConnectionID, s.RepositoryID, d.Event.FullRef, object, d.Event.CommitSHA)
		if err != nil {
			return nil, false, err
		}
	}
	orderedIntents := append([]*model.TriggerIntent(nil), intents...)
	sort.Slice(orderedIntents, func(i, j int) bool { return orderedIntents[i].LogicalKey < orderedIntents[j].LogicalKey })
	for _, intent := range orderedIntents {
		cp := store.CloneTrigger(intent)
		if moved {
			cp.Status = model.TriggerAwaitingReview
			cp.Reason = "release tag moved; verify the new revision before deployment"
		}
		spec, err := json.Marshal(cp)
		if err != nil {
			return nil, false, err
		}
		var id string
		err = tx.QueryRowContext(ctx, `INSERT INTO trigger_intents(id,logical_key,delivery_id,rule_id,status,reason,spec,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8) ON CONFLICT(logical_key) DO NOTHING RETURNING id`, cp.ID, cp.LogicalKey, d.ID, cp.Rule.ID, cp.Status, cp.Reason, spec, cp.CreatedAt).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			old, err := scanTriggerIntent(tx.QueryRowContext(ctx, `SELECT `+triggerIntentColumns+` FROM trigger_intents WHERE logical_key=$1`, cp.LogicalKey))
			if err != nil {
				return nil, false, err
			}
			store.ApplyTriggerIntentDecision(copy, old, true, cp.ID)
			continue
		}
		if err != nil {
			return nil, false, err
		}
		store.ApplyTriggerIntentDecision(copy, cp, false)
	}
	for index := range copy.Decisions {
		decision := &copy.Decisions[index]
		decision.AppID = ""
		decision.EnvironmentID = ""
		decision.TargetKind = ""
		decision.TargetID = ""
		if target, ok := targets[decision.RuleID]; ok {
			decision.AppID = target.AppID
			decision.EnvironmentID = target.EnvironmentID
			decision.TargetKind, decision.TargetID = target.Target()
		}
		minimal := store.MinimalTriggerDecision(copy.ID, index+1, *decision)
		if _, err = tx.ExecContext(ctx, `INSERT INTO trigger_delivery_decisions(delivery_id,decision_index,rule_id,rule_version,app_id,environment_id,target_kind,target_id,intent_id,outcome,reason_code) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, minimal.DeliveryID, minimal.DecisionIndex, minimal.RuleID, minimal.RuleVersion, minimal.AppID, minimal.EnvironmentID, minimal.TargetKind, minimal.TargetID, minimal.IntentID, minimal.Outcome, minimal.ReasonCode); err != nil {
			return nil, false, err
		}
	}
	decisionJSON, err := json.Marshal(copy.Decisions)
	if err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE webhook_deliveries SET decisions=$2 WHERE id=$1`, d.ID, decisionJSON); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return copy, false, nil
}

func (t *TriggerStore) PendingIntents(ctx context.Context, limit int) ([]*model.TriggerIntent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := t.db.QueryContext(ctx, `SELECT `+triggerIntentColumns+` FROM trigger_intents WHERE status='pending' ORDER BY created_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.TriggerIntent
	for rows.Next() {
		i, err := scanTriggerIntent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (t *TriggerStore) ListIntents(ctx context.Context, appID string, limit int) ([]*model.TriggerIntent, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := t.db.QueryContext(ctx, `SELECT `+triggerIntentColumns+` FROM trigger_intents WHERE ($1='' OR spec->'rule'->>'appId'=$1) ORDER BY created_at DESC,id LIMIT $2`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.TriggerIntent{}
	for rows.Next() {
		intent, err := scanTriggerIntent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, intent)
	}
	return out, rows.Err()
}

func (t *TriggerStore) TransitionIntent(ctx context.Context, id string, version int, status model.TriggerIntentStatus, reason string, receipt *model.TriggerReceipt) error {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // Commit/operation errors determine the result.
	current, err := scanTriggerIntent(tx.QueryRowContext(ctx, `SELECT `+triggerIntentColumns+` FROM trigger_intents WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	if current.Version != version {
		return store.ErrConflict
	}
	if err := store.ValidateTriggerTransition(current, status, reason, receipt); err != nil {
		return err
	}
	b, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE trigger_intents SET version=version+1,status=$2,reason=$3,receipt=$4,updated_at=NOW() WHERE id=$1`, id, status, reason, b)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// PruneTriggers compacts audit data while preserving the authenticated event,
// logical keys and receipts. These tombstones prevent old deliveries from
// becoming new deployments merely because their rich history expired.
func (t *TriggerStore) PruneTriggers(ctx context.Context, before time.Time) (int64, error) {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // Commit/operation errors determine the result.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM webhook_deliveries d WHERE d.created_at<$1 AND NOT archived AND NOT EXISTS(SELECT 1 FROM trigger_intents i WHERE i.delivery_id=d.id AND status IN('pending','awaiting_review')) ORDER BY created_at LIMIT 1000 FOR UPDATE SKIP LOCKED`, before)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		_, err = tx.ExecContext(ctx, `UPDATE trigger_intents SET spec=jsonb_set(spec,'{rule}',jsonb_build_object('id',spec->'rule'->'id','version',spec->'rule'->'version','sourceVersion',spec->'rule'->'sourceVersion','targetKind',spec->'rule'->'targetKind','targetId',spec->'rule'->'targetId','pipelineVersion',spec->'rule'->'pipelineVersion','environmentVersions',spec->'rule'->'environmentVersions','reviewedBySubject',spec->'rule'->'reviewedBySubject','reviewedAt',spec->'rule'->'reviewedAt','appId',spec->'rule'->'appId','environmentId',spec->'rule'->'environmentId','appVersion',spec->'rule'->'appVersion','environmentVersion',spec->'rule'->'environmentVersion','planHash',spec->'rule'->'planHash','approvedEnvelopeHash',spec->'rule'->'approvedEnvelopeHash')),archived=TRUE WHERE delivery_id=$1 AND status NOT IN('pending','awaiting_review')`, id)
		if err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE webhook_deliveries SET decisions='[]',archived=TRUE WHERE id=$1`, id); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

var _ store.TriggerStore = (*TriggerStore)(nil)
