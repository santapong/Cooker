package vercel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Postgres struct{ DB *sql.DB }

const columns = `id,configuration,credential,version,snapshot,last_attempt_at,last_success_at,next_sync_at,retry_at,sync_error,sync_lease,sync_lease_until,failures,created_at,updated_at`

func scan(row interface{ Scan(...any) error }) (*Record, error) {
	var config, snapshot, credential []byte
	var id string
	var version, failures int
	var attempt, success, next, retry, leaseUntil *time.Time
	var syncErr string
	var lease sql.NullString
	var created, updated time.Time
	if err := row.Scan(&id, &config, &credential, &version, &snapshot, &attempt, &success, &next, &retry, &syncErr, &lease, &leaseUntil, &failures, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r := &Record{}
	if err := json.Unmarshal(config, &r.Connection); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(snapshot, &r.Snapshot); err != nil {
		return nil, err
	}
	r.ID = id
	r.Credential = credential
	r.Version = version
	r.LastAttemptAt = attempt
	r.LastSuccessAt = success
	r.NextSyncAt = next
	r.RetryAt = retry
	r.SyncError = syncErr
	r.Lease = lease.String
	r.LeaseUntil = leaseUntil
	r.Failures = failures
	r.CreatedAt = created
	r.UpdatedAt = updated
	r.HasToken = len(credential) > 0
	return r, nil
}
func (p *Postgres) List(ctx context.Context) ([]*Record, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+columns+` FROM vercel_connections ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Record{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (p *Postgres) Get(ctx context.Context, id string) (*Record, error) {
	return scan(p.DB.QueryRowContext(ctx, `SELECT `+columns+` FROM vercel_connections WHERE id=$1`, id))
}
func (p *Postgres) Create(ctx context.Context, r *Record) error {
	config, err := json.Marshal(&r.Connection)
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(&r.Snapshot)
	if err != nil {
		return err
	}
	_, err = p.DB.ExecContext(ctx, `INSERT INTO vercel_connections(id,configuration,credential,version,snapshot,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.ID, config, r.Credential, r.Version, snapshot, r.CreatedAt, r.UpdatedAt)
	return err
}
func (p *Postgres) Update(ctx context.Context, r *Record, version int) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM vercel_connections WHERE id=$1 FOR UPDATE`, r.ID))
	if err != nil {
		return err
	}
	if current.Version != version {
		return ErrConflict
	}
	next := applyConfig(current, r)
	config, err := json.Marshal(&next.Connection)
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(&next.Snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE vercel_connections SET configuration=$2,credential=$3,version=$4,snapshot=$5,last_success_at=$6,next_sync_at=$7,sync_error=$8,sync_lease=NULL,sync_lease_until=NULL,updated_at=$9 WHERE id=$1`, r.ID, config, next.Credential, next.Version, snapshot, next.LastSuccessAt, next.NextSyncAt, next.SyncError, next.UpdatedAt)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		r.Version = next.Version
	}
	return err
}
func (p *Postgres) Delete(ctx context.Context, id string, version int) error {
	res, err := p.DB.ExecContext(ctx, `DELETE FROM vercel_connections WHERE id=$1 AND version=$2`, id, version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}
func (p *Postgres) Claim(ctx context.Context, id string, version int, now, until time.Time, manual bool) (string, error) {
	lease := uuid.NewString()
	res, err := p.DB.ExecContext(ctx, `UPDATE vercel_connections SET sync_lease=$3,sync_lease_until=$4,last_attempt_at=$5 WHERE id=$1 AND version=$2 AND (configuration->>'enabled')::boolean AND jsonb_array_length(configuration->'projectIds')>0 AND (sync_lease_until IS NULL OR sync_lease_until<=$5) AND (retry_at IS NULL OR retry_at<=$5) AND ($6 OR next_sync_at IS NULL OR next_sync_at<=$5)`, id, version, lease, until, now, manual)
	if err != nil {
		return "", err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", ErrBusy
	}
	return lease, nil
}
func (p *Postgres) Finish(ctx context.Context, id string, version int, lease string, snapshot *Snapshot, message string, next time.Time, retry *time.Time) error {
	var data any
	var success *time.Time
	if snapshot != nil {
		b, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		data = b
		now := time.Now().UTC()
		success = &now
	}
	res, err := p.DB.ExecContext(ctx, `UPDATE vercel_connections SET snapshot=COALESCE($4::jsonb,snapshot),sync_error=$5,next_sync_at=$6,retry_at=$7,last_success_at=COALESCE($8,last_success_at),failures=CASE WHEN $8::timestamptz IS NOT NULL THEN 0 ELSE failures+1 END,sync_lease=NULL,sync_lease_until=NULL WHERE id=$1 AND version=$2 AND sync_lease=$3 AND sync_lease_until>NOW()`, id, version, lease, data, message, next, retry, success)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}
