package vercel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type PostgresLandingUpdateReviews struct{ DB *sql.DB }

const landingUpdateReviewColumns = `id,adoption_id,adoption_version,connection_id,connection_version,account_scope,account_id,team_id,project_id,environment,actor,receipt,created_at,expires_at`

func scanLandingUpdateReview(row interface{ Scan(...any) error }) (*LandingUpdateReview, error) {
	var r LandingUpdateReview
	var id, adoptionID, connectionID, scope, accountID, teamID, projectID, environment, actor string
	var adoptionVersion, connectionVersion int
	var data []byte
	var created, expires time.Time
	if err := row.Scan(&id, &adoptionID, &adoptionVersion, &connectionID, &connectionVersion, &scope, &accountID, &teamID, &projectID, &environment, &actor, &data, &created, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if json.Unmarshal(data, &r) != nil {
		return nil, ErrInvalid
	}
	r.Actor = actor
	if validateLandingUpdateReview(&r) != nil || r.ID != id || r.AdoptionID != adoptionID || r.AdoptionVersion != adoptionVersion || r.ConnectionID != connectionID || r.ConnectionVersion != connectionVersion || r.AccountScope != scope || r.AccountID != accountID || r.TeamID != teamID || r.ProjectID != projectID || r.Environment != environment || !r.CreatedAt.Equal(created) || !r.ExpiresAt.Equal(expires) {
		return nil, ErrInvalid
	}
	return &r, nil
}

func (p *PostgresLandingUpdateReviews) Get(ctx context.Context, id string) (*LandingUpdateReview, error) {
	if p == nil || p.DB == nil {
		return nil, ErrUnavailable
	}
	return scanLandingUpdateReview(p.DB.QueryRowContext(ctx, `SELECT `+landingUpdateReviewColumns+` FROM vercel_landing_update_reviews WHERE id=$1`, id))
}

func (p *PostgresLandingUpdateReviews) List(ctx context.Context) ([]*LandingUpdateReview, error) {
	if p == nil || p.DB == nil {
		return nil, ErrUnavailable
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT `+landingUpdateReviewColumns+` FROM vercel_landing_update_reviews ORDER BY created_at DESC,id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LandingUpdateReview{}
	for rows.Next() {
		r, err := scanLandingUpdateReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PostgresLandingUpdateReviews) Create(ctx context.Context, r *LandingUpdateReview) error {
	if p == nil || p.DB == nil {
		return ErrUnavailable
	}
	if err := validateLandingUpdateReview(r); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Authoritative connection first, then adoption; no network calls under SQL
	// locks. Both pins are checked again before the immutable receipt insert.
	connection, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM vercel_connections WHERE id=$1 FOR SHARE`, r.ConnectionID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		return err
	}
	a, err := scanLandingAdoption(tx.QueryRowContext(ctx, `SELECT `+adoptionColumns+` FROM vercel_landing_adoptions WHERE id=$1 FOR SHARE`, r.AdoptionID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		return err
	}
	if !reviewAdoptionMatches(a, r) || !adoptionConnectionMatches(connection, a) || !r.ExpiresAt.After(time.Now().UTC()) || r.CreatedAt.After(time.Now().UTC().Add(time.Microsecond)) {
		return ErrConflict
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	next := cloneLandingUpdateReview(r)
	next.CreatedAt = next.CreatedAt.Truncate(time.Microsecond)
	next.ExpiresAt = next.CreatedAt.Add(LandingUpdateReviewTTL)
	if err = validateLandingUpdateReview(next); err != nil {
		return err
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO vercel_landing_update_reviews(id,adoption_id,adoption_version,connection_id,connection_version,account_scope,account_id,team_id,project_id,environment,actor,receipt,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, next.ID, next.AdoptionID, next.AdoptionVersion, next.ConnectionID, next.ConnectionVersion, next.AccountScope, next.AccountID, next.TeamID, next.ProjectID, next.Environment, next.Actor, data, next.CreatedAt, next.ExpiresAt)
	if err != nil {
		return adoptionWriteError(err)
	}
	if err = tx.Commit(); err != nil {
		return adoptionWriteError(err)
	}
	r.CreatedAt, r.ExpiresAt = next.CreatedAt, next.ExpiresAt
	return nil
}
