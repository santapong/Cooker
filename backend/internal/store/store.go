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
	// UpdateStatus writes only the lifecycle columns (status, finished_at,
	// error). It must not re-marshal the JSONB blobs (stage_runs / env /
	// variables) or touch heartbeat_at. Used by status-only transitions
	// (e.g. cancel) so they avoid a full-row rewrite (F18) and never clobber
	// the coordinator's heartbeat — the prerequisite for safely wiring
	// mid-run progress persistence (F2). See
	// docs/proposals/run-state-concurrency-2026.md.
	UpdateStatus(ctx context.Context, id string, status model.RunStatus, finishedAt *time.Time, errMsg string) error
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

// PromotionStore persists run→environment promotions and their
// approvals (audit findings HS26-05-01 / -08 / -14). A promotion is
// keyed uniquely by (run, environment); approvals are append-only with
// a one-per-approver uniqueness guarantee so the count drives manual
// gate completion. See docs/adr/0005-promotion-approval-persistence.md.
type PromotionStore interface {
	// CreatePromotion inserts a new promotion. Returns ErrConflict if a
	// promotion for the same (run, environment) already exists.
	CreatePromotion(ctx context.Context, p *model.RunPromotion) error
	// GetPromotion returns the promotion for (runID, environmentID) with
	// its Approvals populated. ErrNotFound if none exists.
	GetPromotion(ctx context.Context, runID, environmentID string) (*model.RunPromotion, error)
	// ListPromotions returns all promotions for a run, each with its
	// Approvals populated, ordered by creation time.
	ListPromotions(ctx context.Context, runID string) ([]*model.RunPromotion, error)
	// UpdatePromotionStatus advances a promotion's status (and stamps
	// promoted_at when non-nil). ErrNotFound if the row is gone.
	UpdatePromotionStatus(ctx context.Context, id string, status model.PromotionStatus, promotedAt *time.Time) error
	// AddApproval records an approval. It is idempotent per approver:
	// re-approval by the same ApproverSub is a no-op (added=false) rather
	// than an error. Returns the resulting distinct-approval count.
	AddApproval(ctx context.Context, a *model.PromotionApproval) (added bool, count int, err error)
}

// StageApprovalStore persists per-stage approval gates and their votes
// (audit finding HS26-05-03). A gate is keyed uniquely by (run, stage);
// votes are append-only with a one-per-approver uniqueness guarantee so
// the count drives the gate's approval threshold. Mirrors PromotionStore.
// See docs/adr/0005-promotion-approval-persistence.md.
type StageApprovalStore interface {
	// CreateGate inserts a new approval gate. Returns ErrConflict if a gate
	// for the same (run, stage) already exists.
	CreateGate(ctx context.Context, g *model.StageApproval) error
	// GetGate returns the gate for (runID, stageID) with its Votes
	// populated. ErrNotFound if none exists.
	GetGate(ctx context.Context, runID, stageID string) (*model.StageApproval, error)
	// ListGates returns all gates for a run, each with its Votes populated,
	// ordered by creation time.
	ListGates(ctx context.Context, runID string) ([]*model.StageApproval, error)
	// UpdateGateStatus advances a gate's status (and stamps resolved_at +
	// resolved_by when non-nil). ErrNotFound if the row is gone.
	UpdateGateStatus(ctx context.Context, id string, status model.StageApprovalStatus, resolvedBy string, resolvedAt *time.Time) error
	// AddVote records an approval. It is idempotent per approver:
	// re-approval by the same ApproverSub is a no-op (added=false). Returns
	// the resulting distinct-approval count.
	AddVote(ctx context.Context, v *model.StageApprovalVote) (added bool, count int, err error)
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
	// UpdateDeployedURL writes only the app's public URL (set by
	// AppDeployer after a successful deploy when the reverse-proxy /
	// port-derived URL is known). ErrNotFound if the App is gone.
	UpdateDeployedURL(ctx context.Context, id, url string) error
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

// AppCanaryStore persists the live state of canary rollouts (OR-1).
// At most one progressing canary exists per app at a time; the service
// guards this and a partial unique index (migration 025) enforces it at
// the DB level. Rows are created when a canary deploy starts, updated as
// the rollout progresses, and stamped terminal on promote / abort /
// failure. Unlike CanaryConfig (intent on the App), AppCanary is observed
// progress driven by service.CanaryService — never set by the user.
type AppCanaryStore interface {
	// Create inserts a new canary state row. Returns ErrConflict when a
	// progressing canary already exists for the same app (the partial
	// unique index rejects the second insert).
	Create(ctx context.Context, c *model.AppCanary) error
	// Get returns a canary by id. ErrNotFound if absent.
	Get(ctx context.Context, id string) (*model.AppCanary, error)
	// GetActive returns the single progressing canary for an app, or
	// ErrNotFound when the app has no canary in flight.
	GetActive(ctx context.Context, appID string) (*model.AppCanary, error)
	// Update writes the mutable fields (weight, status, healthy, message,
	// promote_after, resolved_at) of an existing canary. ErrNotFound if
	// the row is gone.
	Update(ctx context.Context, c *model.AppCanary) error
	// ClaimTerminal atomically transitions a progressing canary to a
	// terminal status, returning true iff THIS caller won the transition.
	// A false return means the row was already resolved by a concurrent
	// actor (a sweeper on another replica, or an operator) — the caller
	// must NOT perform the traffic change. Guards the promote/abort race
	// (PM26-07-02). ErrNotFound if the row does not exist at all.
	ClaimTerminal(ctx context.Context, id string, to model.CanaryStatus) (bool, error)
	// ListProgressing returns every canary still in the progressing state
	// across all apps, oldest-first. Backs the auto-promote sweep.
	ListProgressing(ctx context.Context) ([]*model.AppCanary, error)
	// DeleteStalePending removes 'pending' canary rows whose StartedAt is
	// before olderThan, returning the count. A pending row is a Start that
	// reserved the one-per-app slot but never reached progressing/failed
	// (process crash or a failed terminal write). Reaping frees the slot
	// that the widened unique index would otherwise hold forever
	// (PM26-07-06 recovery). Backs the auto-promote sweep.
	DeleteStalePending(ctx context.Context, olderThan time.Time) (int, error)
	// LatestPromoted returns the app's most recently promoted canary
	// (by resolved_at), or ErrNotFound when the app has never promoted
	// one. Backs stable-image resolution: after a promote, the promoted
	// canary image is what the app is serving (PM26-07-01).
	LatestPromoted(ctx context.Context, appID string) (*model.AppCanary, error)
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

// RegistryConfigStore persists Settings-page image-registry
// connections (audit finding HS26-05-04). Mirrors the HostStore shape;
// there is no Update — the Settings UI replaces a registry by
// delete+add rather than editing in place. Secret material (the auth
// password/token) is held in secrets.Manager by the service layer; only
// the reference lands on the row.
type RegistryConfigStore interface {
	List(ctx context.Context) ([]*model.RegistryConfig, error)
	Get(ctx context.Context, id string) (*model.RegistryConfig, error)
	Create(ctx context.Context, r *model.RegistryConfig) error
	Delete(ctx context.Context, id string) error
}

// ClusterConfigStore persists Settings-page Kubernetes cluster
// connections (audit finding HS26-05-04). Same shape and rationale as
// RegistryConfigStore; the kubeconfig body is the secret held in
// secrets.Manager.
type ClusterConfigStore interface {
	List(ctx context.Context) ([]*model.ClusterConfig, error)
	Get(ctx context.Context, id string) (*model.ClusterConfig, error)
	Create(ctx context.Context, cfg *model.ClusterConfig) error
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

// APITokenStore persists personal-access / service-account tokens
// (product-plan Tier 1). A token is a long-lived bearer credential
// (`ck_<base64url>`) scripts and external CI use in place of the browser
// OIDC flow. Only the SHA-256 hash of the plaintext is stored; the
// plaintext is shown exactly once at creation and revocation is a row
// delete. GetByHash is on the auth hot path (every ck_-prefixed request)
// and is served by a unique index on the hash column (migration 023).
type APITokenStore interface {
	// Create inserts a new token. The caller is responsible for having
	// hashed the plaintext and set DisplayPrefix before calling.
	Create(ctx context.Context, t *model.APIToken) error
	// ListByCreator returns the tokens minted by one user (keyed on
	// CreatedBySub), newest-first. Backs "list my tokens".
	ListByCreator(ctx context.Context, createdBySub string) ([]*model.APIToken, error)
	// ListAll returns every token, newest-first. Admin-only at the
	// service layer.
	ListAll(ctx context.Context) ([]*model.APIToken, error)
	// GetByHash looks a token up by its hex SHA-256 hash. ErrNotFound if
	// no token has that hash. This is the auth-middleware lookup.
	GetByHash(ctx context.Context, hash string) (*model.APIToken, error)
	// Get returns a token by id. ErrNotFound if absent. Used by the
	// delete path to resolve ownership before deleting.
	Get(ctx context.Context, id string) (*model.APIToken, error)
	// Delete removes a token by id (revocation). ErrNotFound if absent.
	Delete(ctx context.Context, id string) error
	// TouchLastUsed stamps last_used_at = ts for the token. Called from
	// the auth hot path, throttled to at most once per minute per token
	// by the middleware so this stays a rare write.
	TouchLastUsed(ctx context.Context, id string, ts time.Time) error
}

// LicenseStore persists the single installed self-hosted license (M2
// self-hosted licensing — docs/launch/01-billing-monetization.md §4).
// There is at most one active license per Cooker instance: Set replaces
// whatever is installed, GetActive returns it (or ErrNotFound when none
// is installed), and Delete clears it. The store keeps only the decoded
// claims plus the signed RawToken; signature verification is the
// licensing service's job (M2-T2), not the store's.
//
// Single-active-row sentinel-id contract: there is exactly one row, keyed
// on the fixed sentinel id "active". Every implementation MUST normalise
// the stored license's ID to "active" on Set — overwriting whatever the
// caller passed (e.g. a UUID from service.Install) and mutating the
// caller's pointer so the in-memory and Postgres backends are
// indistinguishable for the same input. GetActive therefore always
// returns a License whose ID is "active". Postgres enforces this in the
// schema via the licenses_one_row CHECK (migration 024); the memory impl
// mirrors it in code.
type LicenseStore interface {
	// GetActive returns the currently installed license. ErrNotFound if
	// no license has been installed. The returned license's ID is always
	// the "active" sentinel.
	GetActive(ctx context.Context) (*model.License, error)
	// Set installs l as the active license, replacing any existing one
	// (upsert: single-row semantics). Stamps InstalledAt when zero and
	// normalises l.ID to the "active" sentinel (mutating l) so both
	// backends yield an identical record for the same input.
	Set(ctx context.Context, l *model.License) error
	// Delete removes the installed license, returning the instance to the
	// unlicensed (free/explorer) baseline. Deleting when none is
	// installed is a no-op (no error), matching "clear license" intent.
	Delete(ctx context.Context) error
}

// Store aggregates all data-access interfaces and a cleanup hook.
// Construct with New and pass to the server and handler layers.
type Store struct {
	Pipelines      PipelineStore
	Runs           RunStore
	Environments   EnvironmentStore
	Promotions     PromotionStore
	StageApprovals StageApprovalStore
	Apps           AppStore
	AppDeploys     AppDeployStore
	AppCanaries    AppCanaryStore
	AuditEvents    AuditEventStore
	Hosts          HostStore
	Registries     RegistryConfigStore
	Clusters       ClusterConfigStore
	Users          UserStore
	APITokens      APITokenStore
	Licenses       LicenseStore
	close          func() error
	ping           func(context.Context) error
}

// Components carries the concrete store implementations plus optional
// lifecycle hooks for New. Using a struct (rather than 17 positional
// args) makes each backend's wiring self-documenting and lets callers
// omit the nil Close/Ping without counting argument positions.
type Components struct {
	Pipelines      PipelineStore
	Runs           RunStore
	Environments   EnvironmentStore
	Promotions     PromotionStore
	StageApprovals StageApprovalStore
	Apps           AppStore
	AppDeploys     AppDeployStore
	AppCanaries    AppCanaryStore
	AuditEvents    AuditEventStore
	Hosts          HostStore
	Registries     RegistryConfigStore
	Clusters       ClusterConfigStore
	Users          UserStore
	APITokens      APITokenStore
	Licenses       LicenseStore
	// Close releases driver resources; nil when no cleanup is required
	// (e.g., in-memory stores).
	Close func() error
	// Ping probes liveness; nil for backends without one (Ping then
	// reports healthy unconditionally).
	Ping func(context.Context) error
}

// New builds a Store from its Components.
func New(c Components) *Store {
	return &Store{
		Pipelines:      c.Pipelines,
		Runs:           c.Runs,
		Environments:   c.Environments,
		Promotions:     c.Promotions,
		StageApprovals: c.StageApprovals,
		Apps:           c.Apps,
		AppDeploys:     c.AppDeploys,
		AppCanaries:    c.AppCanaries,
		AuditEvents:    c.AuditEvents,
		Hosts:          c.Hosts,
		Registries:     c.Registries,
		Clusters:       c.Clusters,
		Users:          c.Users,
		APITokens:      c.APITokens,
		Licenses:       c.Licenses,
		close:          c.Close,
		ping:           c.Ping,
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
