package vercel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lib/pq"
)

type PostgresLandingAdoptions struct{ DB *sql.DB }

const adoptionColumns = `id,connection_id,connection_version,version,account_scope,account_id,team_id,project_id,environment,enabled,binding,created_at,updated_at`

func scanLandingAdoption(row interface{ Scan(...any) error }) (*LandingAdoption, error) {
	var a LandingAdoption
	var data []byte
	var id, connectionID, scope, accountID, teamID, projectID, environment string
	var connectionVersion, version int
	var enabled bool
	var created, updated time.Time
	if err := row.Scan(&id, &connectionID, &connectionVersion, &version, &scope, &accountID, &teamID, &projectID, &environment, &enabled, &data, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if json.Unmarshal(data, &a) != nil || validateLandingAdoption(&a) != nil || a.ID != id || a.ConnectionID != connectionID || a.ConnectionVersion != connectionVersion || a.Version != version || a.AccountScope != scope || a.AccountID != accountID || a.TeamID != teamID || a.ProjectID != projectID || a.Environment != environment || a.Enabled != enabled {
		return nil, ErrInvalid
	}
	a.CreatedAt, a.UpdatedAt = created, updated
	return &a, nil
}

func (p *PostgresLandingAdoptions) Get(ctx context.Context, id string) (*LandingAdoption, error) {
	if p == nil || p.DB == nil {
		return nil, ErrUnavailable
	}
	return scanLandingAdoption(p.DB.QueryRowContext(ctx, `SELECT `+adoptionColumns+` FROM vercel_landing_adoptions WHERE id=$1`, id))
}

func (p *PostgresLandingAdoptions) List(ctx context.Context) ([]*LandingAdoption, error) {
	if p == nil || p.DB == nil {
		return nil, ErrUnavailable
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT `+adoptionColumns+` FROM vercel_landing_adoptions ORDER BY created_at DESC,id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LandingAdoption{}
	for rows.Next() {
		if len(out) == maxLandingAdoptions {
			return nil, limitError("landing adoption inventory exceeds the supported limit")
		}
		a, err := scanLandingAdoption(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func adoptionWriteError(err error) error {
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

func lockAdoptionConnection(ctx context.Context, tx *sql.Tx, a *LandingAdoption) error {
	r, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM vercel_connections WHERE id=$1 FOR SHARE`, a.ConnectionID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		return err
	}
	if !adoptionConnectionMatches(r, a) {
		return ErrConflict
	}
	return nil
}

func (p *PostgresLandingAdoptions) Create(ctx context.Context, a *LandingAdoption) error {
	if p == nil || p.DB == nil {
		return ErrUnavailable
	}
	if err := validateLandingAdoption(a); err != nil {
		return err
	}
	if a.Version != 1 || !a.Enabled {
		return ErrInvalid
	}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockAdoptionConnection(ctx, tx, a); err != nil {
		return err
	}
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.Truncate(time.Microsecond), a.UpdatedAt.Truncate(time.Microsecond)
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO vercel_landing_adoptions(id,connection_id,connection_version,version,account_scope,account_id,team_id,project_id,environment,enabled,binding,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, a.ID, a.ConnectionID, a.ConnectionVersion, a.Version, a.AccountScope, a.AccountID, a.TeamID, a.ProjectID, a.Environment, a.Enabled, data, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return adoptionWriteError(err)
	}
	return adoptionWriteError(tx.Commit())
}

func (p *PostgresLandingAdoptions) Update(ctx context.Context, a *LandingAdoption, version int) error {
	if p == nil || p.DB == nil {
		return ErrUnavailable
	}
	if err := validateLandingAdoption(a); err != nil {
		return err
	}
	if a.Version != version {
		return ErrConflict
	}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Connection first, then adoption. Disable needs no live connection and is
	// still possible after disconnect, without changing historical authority.
	if a.Enabled {
		if err = lockAdoptionConnection(ctx, tx, a); err != nil {
			return err
		}
	}
	old, err := scanLandingAdoption(tx.QueryRowContext(ctx, `SELECT `+adoptionColumns+` FROM vercel_landing_adoptions WHERE id=$1 FOR UPDATE`, a.ID))
	if err != nil {
		return err
	}
	if old.Version != version || !sameAdoptionIdentity(old, a) {
		return ErrConflict
	}
	next := cloneLandingAdoption(a)
	next.Version++
	next.UpdatedAt = next.UpdatedAt.Truncate(time.Microsecond)
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE vercel_landing_adoptions SET connection_version=$2,version=$3,enabled=$4,binding=$5,updated_at=$6 WHERE id=$1`, next.ID, next.ConnectionVersion, next.Version, next.Enabled, data, next.UpdatedAt)
	if err != nil {
		return adoptionWriteError(err)
	}
	if err = tx.Commit(); err != nil {
		return adoptionWriteError(err)
	}
	a.Version, a.UpdatedAt = next.Version, next.UpdatedAt
	return nil
}
