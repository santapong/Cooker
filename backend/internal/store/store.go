// Package store defines the data access interfaces for Cooker.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// ErrNotFound is returned by store implementations when a requested
// entity does not exist. Callers should use errors.Is to check.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned by Update methods when the row's version
// has moved since the caller fetched it (optimistic-concurrency
// failure). Handlers should map this to HTTP 409 Conflict and ask
// the client to refetch and retry.
var ErrConflict = errors.New("store: version conflict")

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
	// List returns a pipeline's runs newest-first. limit caps the number
	// of rows (<= 0 means unbounded); offset skips that many newest rows
	// for pagination. Returned runs are summaries: per-stage Logs are
	// omitted so a long history can't balloon the payload — fetch one
	// run via Get (or the stage-logs endpoint) for full logs.
	List(ctx context.Context, pipelineID string, limit, offset int) ([]*model.PipelineRun, error)
	Get(ctx context.Context, id string) (*model.PipelineRun, error)
	Create(ctx context.Context, run *model.PipelineRun) error
	Update(ctx context.Context, run *model.PipelineRun) error
	// UpdateHeartbeat is a cheap UPDATE-one-column write used by the
	// run coordinator's ticker. Implementations should not re-marshal
	// the JSONB columns.
	UpdateHeartbeat(ctx context.Context, id string, ts time.Time) error
	// SweepOrphans marks runs that were status='running' at boot time
	// without a recent heartbeat as failed (they were orphaned by a
	// previous crash). Returns the number of rows updated.
	SweepOrphans(ctx context.Context, threshold time.Duration) (int, error)
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
	// UpdateHealth writes the latest probe verdict for an App without
	// touching Version or any other field. Called from
	// service.AppHealthChecker on its periodic tick. Returns
	// ErrNotFound if the App is gone (probe lost the race with a
	// delete) — callers should log and skip rather than retry.
	// deployedURL may be empty for targets that don't expose an ingress;
	// an empty string leaves a previously-written URL intact in the store.
	UpdateHealth(ctx context.Context, id string, status model.AppHealth, msg string, at time.Time, deployedURL string) error
}

// AppDeployStore manages per-app deploy history (roadmap M3). Rows
// are append-only: AppDeployer records one per terminal deploy or
// rollback; nothing updates them afterwards.
type AppDeployStore interface {
	Create(ctx context.Context, d *model.AppDeploy) error
	// ListByApp returns the app's history newest-first, capped at
	// limit (limit <= 0 means a sane server-side default).
	ListByApp(ctx context.Context, appID string, limit int) ([]*model.AppDeploy, error)
	Get(ctx context.Context, id string) (*model.AppDeploy, error)
}

// AuditQuery filters an audit-trail read (roadmap M5). Zero values
// mean "no filter"; Limit <= 0 falls back to a server-side default.
type AuditQuery struct {
	From       *time.Time
	To         *time.Time
	UserSub    string
	Method     string
	PathPrefix string
	Limit      int
	Offset     int
}

// AuditEventStore persists the queryable audit trail. Insert is
// called from the async db audit sink (never from request
// goroutines); Query backs the admin viewer; DeleteOlderThan is the
// retention sweep.
type AuditEventStore interface {
	Insert(ctx context.Context, e *model.AuditEvent) error
	Query(ctx context.Context, q AuditQuery) ([]*model.AuditEvent, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error)
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
	AppDeploys   AppDeployStore
	AuditEvents  AuditEventStore
	Hosts        HostStore
	Users        UserStore
	close        func() error
	ping         func(context.Context) error
}

// New builds a Store. closeFn may be nil when no cleanup is required
// (e.g., in-memory stores). pingFn may be nil for backends without a
// liveness probe; Ping then reports healthy unconditionally.
func New(p PipelineStore, r RunStore, e EnvironmentStore, a AppStore, ad AppDeployStore, ae AuditEventStore, h HostStore, u UserStore, closeFn func() error, pingFn func(context.Context) error) *Store {
	return &Store{
		Pipelines:    p,
		Runs:         r,
		Environments: e,
		Apps:         a,
		AppDeploys:   ad,
		AuditEvents:  ae,
		Hosts:        h,
		Users:        u,
		close:        closeFn,
		ping:         pingFn,
	}
}

// Close releases resources held by the underlying driver, if any.
func (s *Store) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// Ping reports whether the underlying datastore is reachable. Returns
// nil when no ping function is registered (memory backend).
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.ping == nil {
		return nil
	}
	return s.ping(ctx)
}
