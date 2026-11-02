package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/santapong/cooker/internal/store"
)

type claimWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Lock order is job before domain row, matching cancellation/reconciliation.
// A predicate alone is insufficient: its snapshot can precede revocation.
func withRunClaim(ctx context.Context, db *sql.DB, runID string, write func(claimWriter) error) error {
	return withDomainClaim(ctx, db, "payload->>'runId'", runID, write)
}

func withAppClaim(ctx context.Context, db *sql.DB, appID string, write func(claimWriter) error) error {
	return withDomainClaim(ctx, db, "payload->'app'->>'id'", appID, write)
}

// identityExpr is a constant selected by the wrappers, never user input.
func withDomainClaim(ctx context.Context, db *sql.DB, identityExpr, identity string, write func(claimWriter) error) error {
	claim, ok := store.RunClaimFromContext(ctx)
	if !ok {
		return write(db)
	}
	if claim.JobID == "" || claim.Owner == "" {
		return store.ErrConflict
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // Commit/operation errors determine the result.
	var owned int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM jobs WHERE id=$1 AND locked_by=$2 AND status='running' AND `+identityExpr+`=$3 FOR SHARE`, claim.JobID, claim.Owner, identity).Scan(&owned)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrConflict
	}
	if err != nil {
		return err
	}
	if err = write(tx); err != nil {
		return err
	}
	return tx.Commit()
}
