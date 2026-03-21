// Package store defines the data access interfaces for Cooker.
package store

import (
	"context"

	"github.com/cooker-ci/cooker/internal/model"
)

// PipelineStore manages pipeline persistence.
type PipelineStore interface {
	List(ctx context.Context) ([]*model.Pipeline, error)
	Get(ctx context.Context, id string) (*model.Pipeline, error)
	Create(ctx context.Context, p *model.Pipeline) error
	Update(ctx context.Context, p *model.Pipeline) error
	Delete(ctx context.Context, id string) error
}

// RunStore manages pipeline run persistence.
type RunStore interface {
	List(ctx context.Context, pipelineID string) ([]*model.PipelineRun, error)
	Get(ctx context.Context, id string) (*model.PipelineRun, error)
	Create(ctx context.Context, run *model.PipelineRun) error
	Update(ctx context.Context, run *model.PipelineRun) error
}

// EnvironmentStore manages environment persistence.
type EnvironmentStore interface {
	List(ctx context.Context) ([]*model.Environment, error)
	Get(ctx context.Context, id string) (*model.Environment, error)
	Create(ctx context.Context, env *model.Environment) error
	Update(ctx context.Context, env *model.Environment) error
	Delete(ctx context.Context, id string) error
}
