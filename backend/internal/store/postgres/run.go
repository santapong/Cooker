package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/store"
)

// RunStore implements store.RunStore using PostgreSQL.
type RunStore struct {
	db *sql.DB
}

// NewRunStore creates a PostgreSQL-backed pipeline-run store.
func NewRunStore(db *sql.DB) *RunStore {
	return &RunStore{db: db}
}

func (s *RunStore) List(ctx context.Context, pipelineID string) ([]*model.PipelineRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, pipeline_id, status, stage_runs, env_statuses, variables,
		        started_at, finished_at, error
		   FROM pipeline_runs WHERE pipeline_id = $1 ORDER BY created_at DESC`,
		pipelineID)
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
		        started_at, finished_at, error
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
	// created_at uses DEFAULT NOW() from the schema.
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO pipeline_runs
		  (id, pipeline_id, status, stage_runs, env_statuses, variables, started_at, finished_at, error)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		r.ID, r.PipelineID, string(r.Status), stageJSON, envJSON, varsJSON,
		nullTime(r.StartedAt), nullTime(r.FinishedAt), r.Error)
	if err != nil {
		return fmt.Errorf("creating run: %w", err)
	}
	return nil
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
		        started_at=$6, finished_at=$7, error=$8
		  WHERE id=$1`,
		r.ID, string(r.Status), stageJSON, envJSON, varsJSON,
		nullTime(r.StartedAt), nullTime(r.FinishedAt), r.Error)
	if err != nil {
		return fmt.Errorf("updating run: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("run %s: %w", r.ID, store.ErrNotFound)
	}
	return nil
}

func scanRun(row scannable) (*model.PipelineRun, error) {
	r := &model.PipelineRun{}
	var status string
	var stageJSON, envJSON, varsJSON []byte
	var started, finished sql.NullTime
	var errStr sql.NullString
	if err := row.Scan(&r.ID, &r.PipelineID, &status, &stageJSON, &envJSON, &varsJSON,
		&started, &finished, &errStr); err != nil {
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
	if started.Valid {
		t := started.Time
		r.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time
		r.FinishedAt = &t
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
