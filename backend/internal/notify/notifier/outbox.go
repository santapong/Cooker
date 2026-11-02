package notifier

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/santapong/cooker/internal/observability"
)

// Outbox is independent of deployment workers. Completion is a durable source
// for reconciliation; delivery errors never rewrite the deployment outcome.
type Outbox struct {
	db          *sql.DB
	targets     TargetStore
	registry    *Registry
	SendTimeout time.Duration
	Lease       time.Duration
	PollEvery   time.Duration
}

func NewOutbox(db *sql.DB, targets TargetStore, registry *Registry) *Outbox {
	return &Outbox{db: db, targets: targets, registry: registry,
		SendTimeout: 10 * time.Second, Lease: 30 * time.Second, PollEvery: 5 * time.Second}
}

// Accept returns after the safe event and its matching target versions commit.
// The same event never adopts targets added/replaced after that acceptance.
func (o *Outbox) Accept(ctx context.Context, input Event) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("notification acceptance unavailable")
	}
	defer tx.Rollback()
	if err = o.acceptTx(ctx, tx, input); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return errors.New("notification acceptance outcome unconfirmed")
	}
	return nil
}

func (o *Outbox) acceptTx(ctx context.Context, tx *sql.Tx, input Event) error {
	event, err := SafeEvent(input)
	if err != nil {
		return err
	}
	if event.ID == "" {
		return errors.New("durable notification requires run or canary identity")
	}
	body, _ := json.Marshal(event)
	res, err := tx.ExecContext(ctx, `INSERT INTO notification_events(id,event_type,payload)
	VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`, event.ID, event.Type, body)
	if err != nil {
		return errors.New("notification event persistence unavailable")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil
	}
	// Config is deliberately never selected or copied during acceptance. A row
	// lock makes event/target version selection atomic against replacement.
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,version,event_types FROM notification_targets
	WHERE enabled=TRUE ORDER BY id FOR SHARE`)
	if err != nil {
		return errors.New("notification target selection unavailable")
	}
	type destination struct {
		id, kind string
		version  int64
		filters  pq.StringArray
	}
	var selected []destination
	for rows.Next() {
		var d destination
		if err := rows.Scan(&d.id, &d.kind, &d.version, &d.filters); err != nil {
			rows.Close()
			return errors.New("notification target selection unavailable")
		}
		matched := len(d.filters) == 0
		for _, et := range d.filters {
			if et == string(event.Type) {
				matched = true
			}
		}
		if matched {
			selected = append(selected, d)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errors.New("notification target selection unavailable")
	}
	for _, d := range selected {
		_, err = tx.ExecContext(ctx, `INSERT INTO notification_deliveries(id,event_id,target_id,target_version,kind)
		VALUES($1,$2,$3,$4,$5)`, deliveryID(event.ID, d.id), event.ID, d.id, d.version, d.kind)
		if err != nil {
			return errors.New("notification delivery persistence unavailable")
		}
	}
	return nil
}

const deliveryColumns = `d.id,d.event_id,e.event_type,d.target_id,d.target_version,d.kind,d.state,
d.attempts,d.max_attempts,d.next_attempt_at,d.lease_until,d.last_code,d.last_http_status,
d.duplicate_possible,d.created_at,d.updated_at`

func scanDelivery(row scannable) (Delivery, error) {
	var d Delivery
	err := row.Scan(&d.ID, &d.EventID, &d.EventType, &d.TargetID, &d.TargetVersion, &d.Kind, &d.State,
		&d.Attempts, &d.MaxAttempts, &d.NextAttemptAt, &d.LeaseUntil, &d.LastCode, &d.LastHTTPStatus,
		&d.DuplicatePossible, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

func (o *Outbox) ListDeliveries(ctx context.Context, q DeliveryQuery) (DeliveryPage, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 200 {
		q.Limit = 200
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	page := DeliveryPage{Items: []Delivery{}, Mode: deliveryMode()}
	if err := o.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs j WHERE j.status IN ('succeeded','failed','cancelled')
	AND j.kind IN ('pipeline-run','app-deploy') AND NOT EXISTS(SELECT 1 FROM notification_job_receipts r WHERE r.job_id=j.id)`).Scan(&page.PendingSourceJobs); err != nil {
		return page, errors.New("notification source availability unavailable")
	}
	if err := o.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_source_repairs r JOIN jobs j ON j.id=r.job_id
	WHERE NOT EXISTS(SELECT 1 FROM notification_job_receipts a WHERE a.job_id=j.id)`).Scan(&page.PendingSourceRepairJobs); err != nil {
		return page, errors.New("notification repair availability unavailable")
	}
	where := `WHERE ($1='' OR d.state=$1) AND ($2='' OR d.target_id=$2) AND ($3='' OR e.payload->>'runId'=$3)`
	err := o.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_deliveries d JOIN notification_events e ON e.id=d.event_id `+where, q.State, q.TargetID, q.RunID).Scan(&page.Total)
	if err != nil {
		return page, errors.New("notification delivery history unavailable")
	}
	rows, err := o.db.QueryContext(ctx, `SELECT `+deliveryColumns+` FROM notification_deliveries d JOIN notification_events e ON e.id=d.event_id `+where+` ORDER BY d.created_at DESC,d.id LIMIT $4 OFFSET $5`, q.State, q.TargetID, q.RunID, q.Limit, q.Offset)
	if err != nil {
		return page, errors.New("notification delivery history unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return page, errors.New("notification delivery history unavailable")
		}
		page.Items = append(page.Items, d)
	}
	if rows.Err() != nil {
		return page, errors.New("notification delivery history unavailable")
	}
	return page, nil
}

func (o *Outbox) GetDelivery(ctx context.Context, id string) (Delivery, error) {
	d, err := scanDelivery(o.db.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM notification_deliveries d JOIN notification_events e ON e.id=d.event_id WHERE d.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrDeliveryNotFound
	}
	if err != nil {
		return d, errors.New("notification delivery history unavailable")
	}
	return d, nil
}

func (o *Outbox) claim(ctx context.Context, now time.Time) (*Delivery, error) {
	owner := uuid.NewString()
	lease := o.Lease
	if lease <= o.SendTimeout {
		lease = o.SendTimeout + 20*time.Second
	}
	row := o.db.QueryRowContext(ctx, `WITH picked AS (
	SELECT id FROM notification_deliveries WHERE state IN ('pending','retry_wait') AND next_attempt_at<=$1
	AND attempts<max_attempts ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED)
	UPDATE notification_deliveries d SET state='sending',attempts=d.attempts+1,lease_owner=$2,
	lease_until=$3,updated_at=$1 FROM picked p WHERE d.id=p.id RETURNING d.id,d.event_id`, now, owner, now.Add(lease))
	var id, eventID string
	if err := row.Scan(&id, &eventID); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, errors.New("notification claim unavailable")
	}
	d, err := o.GetDelivery(ctx, id)
	if err != nil {
		return nil, err
	}
	var body []byte
	if err := o.db.QueryRowContext(ctx, `SELECT payload FROM notification_events WHERE id=$1`, eventID).Scan(&body); err != nil {
		return nil, errors.New("notification event unavailable")
	}
	if err := json.Unmarshal(body, &d.event); err != nil {
		return nil, errors.New("notification event unavailable")
	}
	d.owner = owner
	return &d, nil
}

// RecoverExpired cannot know whether a dead worker's receiver accepted a send.
// Never reclaim it for automatic sending. The state is visible for inspection.
func (o *Outbox) RecoverExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := o.db.ExecContext(ctx, `UPDATE notification_deliveries SET state='needs_attention',
	last_code='worker_lost',duplicate_possible=TRUE,lease_owner='',lease_until=NULL,updated_at=$1
	WHERE state='sending' AND lease_until<=$1`, now)
	if err != nil {
		return 0, errors.New("notification lease recovery unavailable")
	}
	return res.RowsAffected()
}

func (o *Outbox) finish(ctx context.Context, d *Delivery, state, code string, status int, duplicate bool, next time.Time) error {
	res, err := o.db.ExecContext(ctx, `UPDATE notification_deliveries SET state=$3,last_code=$4,last_http_status=$5,
	duplicate_possible=(duplicate_possible OR $6),next_attempt_at=$7,lease_owner='',lease_until=NULL,updated_at=NOW()
	WHERE id=$1 AND lease_owner=$2 AND state='sending'`, d.ID, d.owner, state, code, status, duplicate, next)
	if err != nil {
		return errors.New("notification result persistence unavailable; outcome uncertain")
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("notification delivery claim lost; outcome uncertain")
	}
	return nil
}

// ProcessOne never changes a deployment row. Definitive HTTP rejections retry
// at 30s/2m, at most three attempts. Transport timeouts/errors may have reached
// the receiver and are held as needs_attention instead.
func (o *Outbox) ProcessOne(ctx context.Context, now time.Time) (bool, error) {
	d, err := o.claim(ctx, now)
	if err != nil || d == nil {
		return false, err
	}
	state, code, httpStatus, duplicate, next := "delivered", "receiver_accepted", 0, false, now
	target, readErr := o.targets.Get(ctx, d.TargetID)
	var sendErr error
	switch {
	case errors.Is(readErr, ErrTargetNotFound):
		state, code = "cancelled", "target_deleted"
	case readErr != nil:
		sendErr = &SendFailure{Code: "target_read_unavailable", Retryable: true}
	case target.Version != d.TargetVersion || target.Kind != d.Kind:
		state, code = "cancelled", "target_replaced"
	case !target.Matches(d.event.Type):
		state, code = "cancelled", "target_disabled_or_filtered"
	default:
		var owned, currentTarget bool
		if err := o.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_deliveries
		WHERE id=$1 AND lease_owner=$2 AND state='sending' AND lease_until>NOW()),
		EXISTS(SELECT 1 FROM notification_targets WHERE id=$3 AND version=$4 AND kind=$5 AND enabled=TRUE
		AND (cardinality(event_types)=0 OR $6=ANY(event_types)))`, d.ID, d.owner, d.TargetID, d.TargetVersion, d.Kind, string(d.event.Type)).Scan(&owned, &currentTarget); err != nil || !owned {
			return true, errors.New("notification delivery claim could not be verified; no send attempted")
		}
		if !currentTarget {
			state, code = "cancelled", "target_changed_before_send"
			break
		}
		n := o.registry.Lookup(d.Kind)
		if n == nil {
			sendErr = &SendFailure{Code: "adapter_unavailable"}
		} else {
			timeout := o.SendTimeout
			if timeout <= 0 {
				timeout = 10 * time.Second
			}
			sendCtx, cancel := context.WithTimeout(ctx, timeout)
			sendErr = boundedSend(sendCtx, n, target, d.event)
			cancel()
		}
	}
	if sendErr != nil {
		failure := safeFailure(sendErr)
		code, httpStatus = failure.Code, failure.HTTPStatus
		state = "failed"
		if failure.Uncertain {
			state, duplicate = "needs_attention", true
		} else if failure.Retryable && d.Attempts < d.MaxAttempts {
			state = "retry_wait"
			next = now.Add(deliveryBackoff(d.Attempts))
			duplicate = failure.HTTPStatus != 0
		} else if failure.Retryable {
			code = "retry_exhausted"
			duplicate = failure.HTTPStatus != 0
		}
	}
	// Persist with an uncancelled bounded context even if the send timed out.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = o.finish(recordCtx, d, state, code, httpStatus, duplicate, next)
	observability.IncNotifierSent(d.Kind, state == "delivered")
	return true, err
}

func (o *Outbox) Run(ctx context.Context) {
	period := o.PollEvery
	if period <= 0 {
		period = 5 * time.Second
	}
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	sweep := func() {
		if err := o.ReconcileJobs(ctx, 100); err != nil {
			slog.Warn("notifier: terminal notification reconciliation unavailable")
		}
		if _, err := o.RecoverExpired(ctx, time.Now().UTC()); err != nil {
			slog.Warn("notifier: delivery lease recovery unavailable")
		}
		for i := 0; i < 20 && ctx.Err() == nil; i++ {
			worked, err := o.ProcessOne(ctx, time.Now().UTC())
			if err != nil {
				slog.Warn("notifier: delivery processing unavailable; inspect delivery history")
				break
			}
			if !worked {
				break
			}
		}
	}
	sweep()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
