package postgres

import (
	"context"
	"database/sql"
	"errors"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"time"
)

const preflightRunColumns = `id,pipeline_id,status,stage_runs,env_statuses,variables,created_at,started_at,finished_at,error,heartbeat_at,started_by_user_sub,started_by_email,started_by_groups,started_by_token_hash,pipeline_version,source`

func lockedPreflightRun(ctx context.Context, tx *sql.Tx, id string) (*model.PipelineRun, error) {
	r, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+preflightRunColumns+` FROM pipeline_runs WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return r, err
}
func lockedPreflightHistory(ctx context.Context, tx *sql.Tx, f store.AppPreflightFailure) ([]*model.AppDeploy, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,app_id,run_id,pipeline_id,image_ref,digest,status,kind,created_at FROM app_deploys WHERE run_id=$1 OR id=$2 ORDER BY id FOR UPDATE`, f.RunID, store.AppPreflightHistoryID(f.RunID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.AppDeploy
	for rows.Next() {
		d, err := scanAppDeploy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *AppDeployStore) RecordAppPreflightFailure(ctx context.Context, f store.AppPreflightFailure) error {
	claim, ok := store.RunClaimFromContext(ctx)
	if !ok || claim.JobID != "app-run:"+f.RunID || claim.Owner == "" || f.Validate() != nil {
		return store.ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Queue lock first fences cancellation/recovery before any domain read/write.
	var payload []byte
	err = tx.QueryRowContext(ctx, `SELECT payload FROM jobs WHERE id=$1 AND kind='app-deploy' AND status='running' AND locked_by=$2 FOR UPDATE`, claim.JobID, claim.Owner).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrConflict
	}
	if err != nil {
		return err
	}
	accepted, err := store.DecodeAppPreflightFailure(payload)
	if err != nil || !store.SameAppPreflightAcceptance(accepted, f) {
		return store.ErrConflict
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM apps WHERE id=$1 FOR SHARE`, f.AppID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	} else if err != nil {
		return err
	}
	run, err := lockedPreflightRun(ctx, tx, f.RunID)
	if err != nil {
		return err
	}
	if err = store.ValidateAppPreflightRun(f, run); err != nil {
		return err
	}
	var hasLogs bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM stage_log_entries WHERE run_id=$1)`, f.RunID).Scan(&hasLogs); err != nil {
		return err
	}
	if hasLogs {
		return store.ErrConflict
	}
	history, err := lockedPreflightHistory(ctx, tx, f)
	if err != nil {
		return err
	}
	if len(history) > 0 {
		if len(history) != 1 || !store.AppPreflightMarkerValid(f, run, history[0]) {
			return store.ErrConflict
		}
		return tx.Commit()
	}
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	if run.Status == model.RunStatusFailed {
		stamp = *run.FinishedAt
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO app_deploys(id,app_id,run_id,pipeline_id,image_ref,digest,status,kind,created_at) VALUES($1,$2,$3,$4,'','','failed','rejected',$5)`, store.AppPreflightHistoryID(f.RunID), f.AppID, f.RunID, f.PipelineID, stamp)
	if err != nil {
		return err
	}
	if n, e := res.RowsAffected(); e != nil {
		return e
	} else if n != 1 {
		return store.ErrConflict
	}
	if run.Status == model.RunStatusPending {
		res, err = tx.ExecContext(ctx, `UPDATE pipeline_runs SET status='failed',error=$2,finished_at=$3 WHERE id=$1 AND status='pending' AND started_at IS NULL AND finished_at IS NULL`, f.RunID, store.AppPreflightSummary(f.Reason), stamp)
		if err != nil {
			return err
		}
		if n, e := res.RowsAffected(); e != nil {
			return e
		} else if n != 1 {
			return store.ErrConflict
		}
	}
	// Confirm triggers did not rewrite the exact decision/evidence before commit.
	observed, err := lockedPreflightRun(ctx, tx, f.RunID)
	if err != nil {
		return err
	}
	history, err = lockedPreflightHistory(ctx, tx, f)
	if err != nil {
		return err
	}
	if len(history) != 1 || !store.AppPreflightMarkerValid(f, observed, history[0]) {
		return store.ErrConflict
	}
	return tx.Commit()
}
