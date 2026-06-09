package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// RunStore implements store.RunStore using PostgreSQL.
type RunStore struct {
	db *sql.DB
}

// NewRunStore creates a PostgreSQL-backed pipeline-run store.
func NewRunStore(db *sql.DB) *RunStore {
	return &RunStore{db: db}
}

func (s *RunStore) List(ctx context.Context, pipelineID string, limit, offset int) ([]*model.PipelineRun, error) {
	// stage_runs is re-projected with each element's "logs" key removed
	// in SQL so the (up to 1 MiB per stage) log text never leaves
	// Postgres on the list path. LIMIT NULL means "no limit", matching
	// the limit <= 0 contract. The query is served by
	// idx_pipeline_runs_pipeline_created (migration 016).
	var limitArg interface{}
	if limit > 0 {
		limitArg = limit
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, pipeline_id, status,
		        COALESCE(
		          (SELECT jsonb_agg(elem - 'logs' ORDER BY idx)
		             FROM jsonb_array_elements(stage_runs) WITH ORDINALITY AS t(elem, idx)),
		          '[]'::jsonb) AS stage_runs,
		        env_statuses, variables,
		        created_at, started_at, finished_at, error, heartbeat_at,
		        started_by_user_sub, started_by_email, started_by_groups, started_by_token_hash
		   FROM pipeline_runs WHERE pipeline_id = $1
		  ORDER BY created_at DESC
		  LIMIT $2 OFFSET $3`,
		pipelineID, limitArg, offset)
	if err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	defer rows.Close()

	var out []*model.PipelineRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *RunStore) Get(ctx context.Context, id string) (*model.PipelineRun, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, pipeline_id, status, stage_runs, env_statuses, variables,
		        created_at, started_at, finished_at, error, heartbeat_at,
		        started_by_user_sub, started_by_email, started_by_groups, started_by_token_hash
		   FROM pipeline_runs WHERE id = $1`, id)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("run %s: %w", id, store.ErrNotFound)
	}
	return r, err
}

func (s *RunStore) Create(ctx context.Context, r *model.PipelineRun) error {
	stageJSON, err := json.Marshal(r.StageRuns)
	if err != nil {
		return fmt.Errorf("marshal stage_runs: %w", err)
	}
	envJSON, err := json.Marshal(r.EnvironmentStatuses)
	if err != nil {
		return fmt.Errorf("marshal env_statuses: %w", err)
	}
	varsJSON, err := json.Marshal(r.Variables)
	if err != nil {
		return fmt.Errorf("marshal variables: %w", err)
	}
	// created_at: use the caller-supplied value when non-zero so that
	// the memory and Postgres impls are symmetric (memory sets it on
	// Create before calling store). If the caller left it zero, fall
	// back to NOW() so we never store the zero timestamp.
	var createdAt interface{}
	if r.CreatedAt.IsZero() {
		createdAt = nil // triggers DEFAULT NOW() in the SQL below
	} else {
		createdAt = r.CreatedAt
	}
	groupsJSON, err := json.Marshal(nonNilStrings(r.StartedByGroups))
	if err != nil {
		return fmt.Errorf("marshal started_by_groups: %w", err)
	}
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO pipeline_runs
		  (id, pipeline_id, status, stage_runs, env_statuses, variables,
		   created_at, started_at, finished_at, error,
		   started_by_user_sub, started_by_email, started_by_groups, started_by_token_hash)
		 VALUES ($1,$2,$3,$4,$5,$6,COALESCE($7::timestamptz, NOW()),$8,$9,$10,$11,$12,$13,$14)
		 RETURNING created_at`,
		r.ID, r.PipelineID, string(r.Status), stageJSON, envJSON, varsJSON,
		createdAt,
		nullTime(r.StartedAt), nullTime(r.FinishedAt), r.Error,
		r.StartedByUserSub, r.StartedByEmail, groupsJSON, r.StartedByTokenHash).Scan(&r.CreatedAt)
	if err != nil {
		return fmt.Errorf("creating run: %w", err)
	}
	return nil
}

// nonNilStrings returns an empty slice for nil so JSON marshals to []
// (not null). pgx + JSONB accepts both but the empty array matches the
// column DEFAULT '[]'.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *RunStore) Update(ctx context.Context, r *model.PipelineRun) error {
	stageJSON, err := json.Marshal(r.StageRuns)
	if err != nil {
		return fmt.Errorf("marshal stage_runs: %w", err)
	}
	envJSON, err := json.Marshal(r.EnvironmentStatuses)
	if err != nil {
		return fmt.Errorf("marshal env_statuses: %w", err)
	}
	varsJSON, err := json.Marshal(r.Variables)
	if err != nil {
		return fmt.Errorf("marshal variables: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE pipeline_runs
		    SET status=$2, stage_runs=$3, env_statuses=$4, variables=$5,
		        started_at=$6, finished_at=$7, error=$8, heartbeat_at=$9
		  WHERE id=$1`,
		r.ID, string(r.Status), stageJSON, envJSON, varsJSON,
		nullTime(r.StartedAt), nullTime(r.FinishedAt), r.Error, nullTime(r.HeartbeatAt))
	if err != nil {
		return fmt.Errorf("updating run: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("run %s: %w", r.ID, store.ErrNotFound)
	}
	return nil
}

// UpdateHeartbeat writes only the heartbeat_at column. Cheap and safe
// to call from the run coordinator's 30-second ticker without
// thrashing JSONB encoders.
func (s *RunStore) UpdateHeartbeat(ctx context.Context, id string, ts time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE pipeline_runs SET heartbeat_at=$2 WHERE id=$1`, id, ts)
	if err != nil {
		return fmt.Errorf("heartbeat run %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("run %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// SweepOrphans marks rows that were status='running' but whose
// heartbeat is stale (or missing) as failed. Returns rows affected.
// Threshold is the maximum acceptable staleness; rows whose heartbeat
// was last written more than threshold ago are orphans.
func (s *RunStore) SweepOrphans(ctx context.Context, threshold time.Duration) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE pipeline_runs
		    SET status='failed',
		        error='orphaned: heartbeat stale at boot',
		        finished_at=NOW()
		  WHERE status='running'
		    AND (heartbeat_at IS NULL OR heartbeat_at < NOW() - $1::interval)`,
		fmt.Sprintf("%d milliseconds", threshold.Milliseconds()))
	if err != nil {
		return 0, fmt.Errorf("sweep orphans: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func scanRun(row scannable) (*model.PipelineRun, error) {
	r := &model.PipelineRun{}
	var status string
	var stageJSON, envJSON, varsJSON, groupsJSON []byte
	var started, finished, heartbeat sql.NullTime
	var errStr sql.NullString
	if err := row.Scan(&r.ID, &r.PipelineID, &status, &stageJSON, &envJSON, &varsJSON,
		&r.CreatedAt, &started, &finished, &errStr, &heartbeat,
		&r.StartedByUserSub, &r.StartedByEmail, &groupsJSON, &r.StartedByTokenHash); err != nil {
		return nil, err
	}
	r.Status = model.RunStatus(status)
	if err := json.Unmarshal(stageJSON, &r.StageRuns); err != nil {
		return nil, fmt.Errorf("unmarshal stage_runs: %w", err)
	}
	if err := json.Unmarshal(envJSON, &r.EnvironmentStatuses); err != nil {
		return nil, fmt.Errorf("unmarshal env_statuses: %w", err)
	}
	if err := json.Unmarshal(varsJSON, &r.Variables); err != nil {
		return nil, fmt.Errorf("unmarshal variables: %w", err)
	}
	if len(groupsJSON) > 0 {
		if err := json.Unmarshal(groupsJSON, &r.StartedByGroups); err != nil {
			return nil, fmt.Errorf("unmarshal started_by_groups: %w", err)
		}
	}
	if started.Valid {
		t := started.Time
		r.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time
		r.FinishedAt = &t
	}
	if heartbeat.Valid {
		t := heartbeat.Time
		r.HeartbeatAt = &t
	}
	if errStr.Valid {
		r.Error = errStr.String
	}
	return r, nil
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
