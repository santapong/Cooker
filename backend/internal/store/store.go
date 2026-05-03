// Package store defines the data access interfaces for Cooker.
package store

import (
	"context"
	"errors"

	"github.com/cooker-ci/cooker/internal/model"
)

// ErrNotFound is returned by store implementations when a requested
// entity does not exist. Callers should use errors.Is to check.
var ErrNotFound = errors.New("store: not found")

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

// AppStore manages App persistence (Phase 3).
type AppStore interface {
	List(ctx context.Context) ([]*model.App, error)
	Get(ctx context.Context, id string) (*model.App, error)
	GetByRepo(ctx context.Context, repo, branch string) (*model.App, error)
	Create(ctx context.Context, a *model.App) error
	Update(ctx context.Context, a *model.App) error
	Delete(ctx context.Context, id string) error
}

// HostStore manages managed-host persistence (Phase 4).
type HostStore interface {
	List(ctx context.Context) ([]*model.Host, error)
	Get(ctx context.Context, id string) (*model.Host, error)
	Create(ctx context.Context, h *model.Host) error
	Update(ctx context.Context, h *model.Host) error
	Delete(ctx context.Context, id string) error
}

// UserStore manages local-auth account persistence. Only used when
// COOKER_LOCAL_AUTH_ENABLED=true; the OIDC path produces no rows here.
type UserStore interface {
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	GetByID(ctx context.Context, id string) (*model.User, error)
	Create(ctx context.Context, u *model.User) error
	Update(ctx context.Context, u *model.User) error
	Count(ctx context.Context) (int, error)
}

// Store aggregates all data-access interfaces and a cleanup hook.
// Construct with New and pass to the server and handler layers.
type Store struct {
	Pipelines    PipelineStore
	Runs         RunStore
	Environments EnvironmentStore
	Apps         AppStore
	Hosts        HostStore
	Users        UserStore
	close        func() error
}

// New builds a Store. closeFn may be nil when no cleanup is required
// (e.g., in-memory stores).
func New(p PipelineStore, r RunStore, e EnvironmentStore, a AppStore, h HostStore, u UserStore, closeFn func() error) *Store {
	return &Store{
		Pipelines:    p,
		Runs:         r,
		Environments: e,
		Apps:         a,
		Hosts:        h,
		Users:        u,
		close:        closeFn,
	}
}

// Close releases resources held by the underlying driver, if any.
func (s *Store) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}
