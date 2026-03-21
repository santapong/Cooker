package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cooker-ci/cooker/internal/model"
)

// PipelineStore implements store.PipelineStore using PostgreSQL.
type PipelineStore struct {
	db *sql.DB
}

// NewPipelineStore creates a new PostgreSQL-backed pipeline store.
func NewPipelineStore(db *sql.DB) *PipelineStore {
	return &PipelineStore{db: db}
}

func (s *PipelineStore) List(ctx context.Context) ([]*model.Pipeline, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, stages, edges, variables, created_at, updated_at FROM pipelines ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing pipelines: %w", err)
	}
	defer rows.Close()

	var pipelines []*model.Pipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		pipelines = append(pipelines, p)
	}
	return pipelines, rows.Err()
}

func (s *PipelineStore) Get(ctx context.Context, id string) (*model.Pipeline, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, stages, edges, variables, created_at, updated_at FROM pipelines WHERE id = $1`, id)

	p := &model.Pipeline{}
	var stagesJSON, edgesJSON, varsJSON []byte
	err := row.Scan(&p.ID, &p.Name, &p.Description, &stagesJSON, &edgesJSON, &varsJSON, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("pipeline not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("getting pipeline: %w", err)
	}

	json.Unmarshal(stagesJSON, &p.Stages)
	json.Unmarshal(edgesJSON, &p.Edges)
	json.Unmarshal(varsJSON, &p.Variables)
	return p, nil
}

func (s *PipelineStore) Create(ctx context.Context, p *model.Pipeline) error {
	stagesJSON, _ := json.Marshal(p.Stages)
	edgesJSON, _ := json.Marshal(p.Edges)
	varsJSON, _ := json.Marshal(p.Variables)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pipelines (id, name, description, stages, edges, variables, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		p.ID, p.Name, p.Description, stagesJSON, edgesJSON, varsJSON, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Update(ctx context.Context, p *model.Pipeline) error {
	stagesJSON, _ := json.Marshal(p.Stages)
	edgesJSON, _ := json.Marshal(p.Edges)
	varsJSON, _ := json.Marshal(p.Variables)

	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET name=$2, description=$3, stages=$4, edges=$5, variables=$6, updated_at=$7 WHERE id=$1`,
		p.ID, p.Name, p.Description, stagesJSON, edgesJSON, varsJSON, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("updating pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pipelines WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting pipeline: %w", err)
	}
	return nil
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanPipeline(row scannable) (*model.Pipeline, error) {
	p := &model.Pipeline{}
	var stagesJSON, edgesJSON, varsJSON []byte
	err := row.Scan(&p.ID, &p.Name, &p.Description, &stagesJSON, &edgesJSON, &varsJSON, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scanning pipeline: %w", err)
	}
	json.Unmarshal(stagesJSON, &p.Stages)
	json.Unmarshal(edgesJSON, &p.Edges)
	json.Unmarshal(varsJSON, &p.Variables)
	return p, nil
}
