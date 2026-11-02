package neon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PostgresConnections persists only allowlisted configuration/snapshots and a
// separately encrypted management credential. It never performs provider I/O.
type PostgresConnections struct{ DB *sql.DB }

const connectionColumns = `id,configuration,credential,version,snapshot,last_attempt_at,last_success_at,error_code,sync_lease,sync_lease_until,created_at,updated_at`

type storedConnectionConfig struct {
	Name           string   `json:"name"`
	OrganizationID string   `json:"organizationId"`
	AccountMode    string   `json:"accountMode"`
	Enabled        bool     `json:"enabled"`
	ProjectIDs     []string `json:"projectIds"`
}

func connectionConfig(r *ConnectionRecord) ([]byte, error) {
	return json.Marshal(storedConnectionConfig{r.Name, r.OrganizationID, r.AccountMode, r.Enabled, r.ProjectIDs})
}

func scanConnection(row interface{ Scan(...any) error }) (*ConnectionRecord, error) {
	r := &ConnectionRecord{}
	var config, snapshot []byte
	var lease sql.NullString
	err := row.Scan(&r.ID, &config, &r.Credential, &r.Version, &snapshot, &r.LastAttemptAt, &r.LastSuccessAt, &r.ErrorCode, &lease, &r.LeaseUntil, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connectionError("not_found")
	}
	if err != nil {
		return nil, err
	}
	var cfg storedConnectionConfig
	if json.Unmarshal(config, &cfg) != nil || json.Unmarshal(snapshot, &r.Snapshot) != nil {
		return nil, connectionError("unavailable")
	}
	r.Name, r.OrganizationID, r.AccountMode, r.Enabled, r.ProjectIDs = cfg.Name, cfg.OrganizationID, cfg.AccountMode, cfg.Enabled, cfg.ProjectIDs
	r.Lease = lease.String
	if !validConnectionRecord(r) {
		return nil, connectionError("unavailable")
	}
	return r, nil
}

func (p *PostgresConnections) begin(ctx context.Context) (*sql.Tx, error) {
	if p == nil || p.DB == nil {
		return nil, connectionError("unavailable")
	}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='5s'; SET LOCAL lock_timeout='1s'; SET LOCAL idle_in_transaction_session_timeout='10s'`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (p *PostgresConnections) List(ctx context.Context) ([]*ConnectionRecord, error) {
	if p == nil || p.DB == nil {
		return nil, connectionError("unavailable")
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT `+connectionColumns+` FROM neon_connections ORDER BY created_at,id LIMIT 101`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ConnectionRecord{}
	for rows.Next() {
		r, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
		if len(out) > MaxNeonConnections {
			return nil, connectionError("connection_limit")
		}
	}
	return out, rows.Err()
}

func (p *PostgresConnections) Get(ctx context.Context, id string) (*ConnectionRecord, error) {
	if p == nil || p.DB == nil {
		return nil, connectionError("unavailable")
	}
	return scanConnection(p.DB.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM neon_connections WHERE id=$1`, id))
}

func (p *PostgresConnections) Create(ctx context.Context, r *ConnectionRecord) error {
	if !validConnectionRecord(r) || r.Version != 1 {
		return connectionError("invalid_input")
	}
	tx, err := p.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The short table lock makes the count limit atomic across processes.
	if _, err = tx.ExecContext(ctx, `LOCK TABLE neon_connections IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM neon_connections`).Scan(&count); err != nil {
		return err
	}
	if count >= MaxNeonConnections {
		return connectionError("connection_limit")
	}
	config, err := connectionConfig(r)
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(r.Snapshot)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO neon_connections(id,configuration,credential,version,snapshot,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO NOTHING`, r.ID, config, r.Credential, r.Version, snapshot, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return connectionError("conflict")
	}
	return tx.Commit()
}

func (p *PostgresConnections) Update(ctx context.Context, r *ConnectionRecord, version int) error {
	if !validConnectionRecord(r) || version < 1 || r.Version != version {
		return connectionError("invalid_input")
	}
	tx, err := p.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM neon_connections WHERE id=$1 FOR UPDATE`, r.ID))
	if err != nil {
		return err
	}
	if current.Version != version {
		return connectionError("conflict")
	}
	if current.OrganizationID != r.OrganizationID {
		return connectionError("conflict")
	}
	next := applyConnectionConfig(current, r)
	config, err := connectionConfig(next)
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(next.Snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE neon_connections SET configuration=$2,credential=$3,version=$4,snapshot=$5,last_attempt_at=$6,last_success_at=$7,error_code=$8,sync_lease=NULL,sync_lease_until=NULL,updated_at=$9 WHERE id=$1`, r.ID, config, next.Credential, next.Version, snapshot, next.LastAttemptAt, next.LastSuccessAt, next.ErrorCode, next.UpdatedAt)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.Version = next.Version
	return nil
}

func (p *PostgresConnections) Delete(ctx context.Context, id string, version int) error {
	tx, err := p.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM neon_connections WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if current.Version != version {
		return connectionError("conflict")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM neon_connections WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *PostgresConnections) Claim(ctx context.Context, id string, version int, lease string, now, until time.Time) error {
	if !validConnectionID(lease) || now.IsZero() || !until.After(now) || until.Sub(now) > ConnectionLeaseDuration {
		return connectionError("invalid_input")
	}
	tx, err := p.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM neon_connections WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if r.Version != version {
		return connectionError("conflict")
	}
	if !r.Enabled || len(r.ProjectIDs) == 0 {
		return connectionError("paused")
	}
	if r.LeaseUntil != nil && r.LeaseUntil.After(now) {
		return connectionError("busy")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE neon_connections SET sync_lease=$2,sync_lease_until=$3,last_attempt_at=$4 WHERE id=$1`, id, lease, until, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *PostgresConnections) Finish(ctx context.Context, id string, version int, lease string, snapshot *ConnectionSnapshot, code string, now time.Time) error {
	if now.IsZero() || (code != "" && !safeConnectionCode(code)) || (snapshot == nil && code == "") || (snapshot != nil && code != "") {
		return connectionError("invalid_input")
	}
	tx, err := p.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM neon_connections WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if r.Version != version || r.Lease != lease || r.LeaseUntil == nil || !r.LeaseUntil.After(now) || r.LastAttemptAt == nil || now.Before(*r.LastAttemptAt) {
		return connectionError("conflict")
	}
	if !r.Enabled || len(r.ProjectIDs) == 0 {
		return connectionError("paused")
	}
	if snapshot != nil {
		if code != "" || validateConnectionSnapshot(snapshot, r.OrganizationID, r.ProjectIDs) != nil || snapshot.ObservedAt.After(now) {
			return connectionError("invalid_response")
		}
		r.Snapshot = cloneConnectionSnapshot(snapshot)
		r.Snapshot.ObservedAt = now
		r.LastSuccessAt = &now
	}
	data, err := json.Marshal(r.Snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE neon_connections SET snapshot=$2,last_success_at=$3,error_code=$4,sync_lease=NULL,sync_lease_until=NULL WHERE id=$1`, id, data, r.LastSuccessAt, code)
	if err != nil {
		return err
	}
	return tx.Commit()
}

var _ ConnectionStore = (*PostgresConnections)(nil)
