package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/notifier"
	"github.com/santapong/cooker/internal/store"
)

// ErrCanaryUnsupported is returned when a canary is requested for an App
// whose deploy target / configured deployer cannot split traffic. The
// handler maps it to HTTP 422. It wraps the deployer-package sentinel so
// callers can match either with errors.Is.
var ErrCanaryUnsupported = fmt.Errorf("canary: unsupported target: %w", deployer.ErrCanaryUnsupported)

// ErrNoActiveCanary is returned by Promote / Abort / Status when the App
// has no canary in flight. The handler maps it to HTTP 404.
var ErrNoActiveCanary = errors.New("canary: no active canary for app")

// ErrCanaryInFlight is returned by Start when a progressing canary
// already exists for the App. The handler maps it to HTTP 409.
var ErrCanaryInFlight = errors.New("canary: a canary is already in flight for this app")

// canaryAppStore is the narrow App-store slice the canary service needs.
// Declared locally so tests can fake just these methods.
type canaryAppStore interface {
	Get(ctx context.Context, id string) (*model.App, error)
}

// canaryImageBuilder builds and pushes the new image for a canary and
// returns its ref. AppDeployer.BuildAndPushImage satisfies this; tests
// inject a fake that skips the real clone/build.
type canaryImageBuilder interface {
	BuildAndPushImage(ctx context.Context, app *model.App, runID string, logW io.Writer) (string, *model.Pipeline, *model.PipelineRun, error)
}

// canaryDeployHistory is the narrow AppDeploy-store slice used to
// resolve the image an app is currently serving (the canary's rollback
// target). store.AppDeployStore satisfies it. nil disables the source.
type canaryDeployHistory interface {
	ListByApp(ctx context.Context, appID string, limit int) ([]*model.AppDeploy, error)
}

// CanaryService orchestrates canary deployments (OR-1). It builds the
// new image, establishes a weighted traffic split via a WeightedDeployer,
// records the live AppCanary state, and either auto-promotes after the
// health window (SweepAutoPromote) or rolls back. All business logic
// lives here; the handler only parses + authorizes + calls through.
type CanaryService struct {
	apps     canaryAppStore
	canaries store.AppCanaryStore
	deploys  canaryDeployHistory
	builder  canaryImageBuilder
	// weighted is the traffic-splitting deployer. nil when the configured
	// deployer can't split traffic (e.g. Noop) — Start then returns
	// ErrCanaryUnsupported before doing any work.
	weighted deployer.WeightedDeployer
	// prober reports the post-deploy health of the canary workload during
	// the health window. nil falls back to "assume healthy" so a target
	// without a wired probe still auto-promotes after the window.
	prober Prober
	// notifier, when non-nil, receives canary.promoted / .aborted /
	// .failed events. Best-effort — a channel failure never fails the
	// rollout.
	notifier *notifier.Dispatcher
	clock    func() time.Time
	logger   *slog.Logger
}

// CanaryOption configures a CanaryService.
type CanaryOption func(*CanaryService)

// WithCanaryProber registers the health prober used during the window.
func WithCanaryProber(p Prober) CanaryOption {
	return func(s *CanaryService) { s.prober = p }
}

// WithCanaryClock injects a deterministic time source for tests.
func WithCanaryClock(now func() time.Time) CanaryOption {
	return func(s *CanaryService) {
		if now != nil {
			s.clock = now
		}
	}
}

// WithCanaryNotifier registers the dispatcher for canary events.
func WithCanaryNotifier(d *notifier.Dispatcher) CanaryOption {
	return func(s *CanaryService) { s.notifier = d }
}

// NewCanaryService wires the service. weighted may be nil when the
// configured deployer cannot split traffic; Start then surfaces
// ErrCanaryUnsupported. deploys may be nil, disabling deploy-history
// stable-image resolution (the promoted-canary source still applies).
func NewCanaryService(apps canaryAppStore, canaries store.AppCanaryStore, deploys canaryDeployHistory, builder canaryImageBuilder, weighted deployer.WeightedDeployer, opts ...CanaryOption) *CanaryService {
	s := &CanaryService{
		apps:     apps,
		canaries: canaries,
		deploys:  deploys,
		builder:  builder,
		weighted: weighted,
		clock:    time.Now,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// supportsCanary reports whether app's target can run a canary with the
// configured deployer. v1: Kubernetes targets only, and only when a
// weighted deployer is wired.
func (s *CanaryService) supportsCanary(app *model.App) bool {
	return s.weighted != nil && app.DeployTarget.Kind == model.DeployTargetKubernetes
}

// Start begins a canary rollout for app: it builds + pushes the new
// image, establishes the weighted split at the config's weight, and
// records a progressing AppCanary. runID aligns the build run with the
// caller's stub row and the app-run:<runID> WS channel. The returned
// AppCanary reflects the persisted state.
//
// Errors: ErrCanaryUnsupported (422) when the target can't split
// traffic; ErrCanaryInFlight (409) when one is already running.
func (s *CanaryService) Start(ctx context.Context, app *model.App, runID string, logW io.Writer) (*model.AppCanary, error) {
	cfg := app.Canary.Normalize()
	if !cfg.IsCanary() {
		return nil, fmt.Errorf("canary: app %s is not configured for canary deploys", app.ID)
	}
	if !s.supportsCanary(app) {
		return nil, ErrCanaryUnsupported
	}

	// Reserve the one-canary-per-app slot FIRST with a pending row, before
	// building or touching the cluster (PM26-07-06). The partial unique
	// index (migration 026, covering pending+progressing) serializes
	// concurrent Starts: the loser's Create hits ErrConflict → 409 and
	// never mutates the cluster. This replaces the old check-then-act
	// (GetActive pre-check + a Create after the deploy) that let two
	// Starts both deploy before one 409'd.
	now := s.clock()
	c := &model.AppCanary{
		ID:                  uuid.NewString(),
		AppID:               app.ID,
		RunID:               runID,
		Weight:              cfg.Weight,
		Status:              model.CanaryPending,
		AutoPromote:         cfg.AutoPromote,
		HealthWindowSeconds: cfg.HealthWindowSeconds,
		Healthy:             true,
		Message:             "starting canary",
		StartedAt:           now,
	}
	if err := s.canaries.Create(ctx, c); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrCanaryInFlight
		}
		return nil, fmt.Errorf("canary: reserve slot: %w", err)
	}

	stableImage := s.stableImageFor(ctx, app)

	canaryImage, _, _, err := s.builder.BuildAndPushImage(ctx, app, runID, logW)
	if err != nil {
		s.failPending(ctx, app, c, "build image: "+err.Error())
		return nil, fmt.Errorf("canary: build image: %w", err)
	}
	if stableImage == "" {
		// No deploy or promote history: split the new image against itself.
		// Harmless (both tracks serve the same version) and keeps the
		// rendered Deployment's image field non-empty (PM26-07-01).
		stableImage = canaryImage
	}

	if _, err := s.weighted.DeployWeighted(ctx, deployer.WeightedRequest{
		Namespace:   app.DeployTarget.Namespace,
		Name:        app.Name,
		StableImage: stableImage,
		CanaryImage: canaryImage,
		Weight:      cfg.Weight,
		LogWriter:   logW,
	}); err != nil {
		s.failPending(ctx, app, c, "establish split: "+err.Error())
		if errors.Is(err, deployer.ErrCanaryUnsupported) {
			return nil, ErrCanaryUnsupported
		}
		return nil, fmt.Errorf("canary: establish split: %w", err)
	}

	// Split is live — promote the reserved row to progressing with the
	// resolved images and the auto-promote deadline.
	c.StableImage = stableImage
	c.CanaryImage = canaryImage
	c.Status = model.CanaryProgressing
	c.Message = fmt.Sprintf("canary at %d%%", cfg.Weight)
	if cfg.AutoPromote {
		t := now.Add(time.Duration(cfg.HealthWindowSeconds) * time.Second)
		c.PromoteAfter = &t
	}
	if err := s.canaries.Update(ctx, c); err != nil {
		return nil, fmt.Errorf("canary: persist state: %w", err)
	}
	s.logger.Info("canary started", "app", app.ID, "weight", cfg.Weight,
		"autoPromote", cfg.AutoPromote, "image", canaryImage)
	return c, nil
}

// Promote shifts all traffic to the canary version (weight 100) and
// marks the canary promoted. Idempotent-ish: a no active canary returns
// ErrNoActiveCanary. The new image becomes the stable image for the next
// rollout implicitly (it's serving 100%).
func (s *CanaryService) Promote(ctx context.Context, appID string) (*model.AppCanary, error) {
	app, c, err := s.loadActive(ctx, appID)
	if err != nil {
		return nil, err
	}
	// Claim the transition BEFORE mutating the cluster (PM26-07-02). If a
	// concurrent sweeper (another replica) or operator already resolved
	// this canary, we lose the CAS and must not issue a second, possibly
	// contradictory, DeployWeighted — return the resolved row as a no-op.
	won, err := s.canaries.ClaimTerminal(ctx, c.ID, model.CanaryPromoted)
	if err != nil {
		return nil, fmt.Errorf("canary: claim promote: %w", err)
	}
	if !won {
		return s.canaries.Get(ctx, c.ID)
	}
	if _, err := s.weighted.DeployWeighted(ctx, deployer.WeightedRequest{
		Namespace:   app.DeployTarget.Namespace,
		Name:        app.Name,
		StableImage: c.StableImage,
		CanaryImage: c.CanaryImage,
		Weight:      100,
		LogWriter:   io.Discard,
	}); err != nil {
		return nil, fmt.Errorf("canary: promote split: %w", err)
	}
	now := s.clock()
	c.Status = model.CanaryPromoted
	c.Weight = 100
	c.Healthy = true
	c.Message = "promoted to 100%"
	c.PromoteAfter = nil
	c.ResolvedAt = &now
	if err := s.canaries.Update(ctx, c); err != nil {
		return nil, fmt.Errorf("canary: persist promote: %w", err)
	}
	s.logger.Info("canary promoted", "app", appID, "image", c.CanaryImage)
	NotifyCanary(s.notifier, app, c, notifier.EventCanaryPromoted, "")
	return c, nil
}

// Abort rolls traffic back to the stable version (weight 0, canary pods
// scaled to zero) and marks the canary aborted. reason is recorded on
// the row for the UI. ErrNoActiveCanary when none is in flight.
func (s *CanaryService) Abort(ctx context.Context, appID, reason string) (*model.AppCanary, error) {
	app, c, err := s.loadActive(ctx, appID)
	if err != nil {
		return nil, err
	}
	// Same CAS guard as Promote (PM26-07-02): claim the transition before
	// scaling the split down, and no-op if another actor already resolved.
	won, err := s.canaries.ClaimTerminal(ctx, c.ID, model.CanaryAborted)
	if err != nil {
		return nil, fmt.Errorf("canary: claim abort: %w", err)
	}
	if !won {
		return s.canaries.Get(ctx, c.ID)
	}
	if _, err := s.weighted.DeployWeighted(ctx, deployer.WeightedRequest{
		Namespace:   app.DeployTarget.Namespace,
		Name:        app.Name,
		StableImage: c.StableImage,
		CanaryImage: c.CanaryImage,
		Weight:      0,
		LogWriter:   io.Discard,
	}); err != nil {
		return nil, fmt.Errorf("canary: abort split: %w", err)
	}
	now := s.clock()
	c.Status = model.CanaryAborted
	c.Weight = 0
	c.Message = reason
	if c.Message == "" {
		c.Message = "aborted by operator"
	}
	c.PromoteAfter = nil
	c.ResolvedAt = &now
	if err := s.canaries.Update(ctx, c); err != nil {
		return nil, fmt.Errorf("canary: persist abort: %w", err)
	}
	s.logger.Info("canary aborted", "app", appID, "reason", c.Message)
	NotifyCanary(s.notifier, app, c, notifier.EventCanaryAborted, c.Message)
	return c, nil
}

// Status returns the active canary for an app, or ErrNoActiveCanary.
func (s *CanaryService) Status(ctx context.Context, appID string) (*model.AppCanary, error) {
	c, err := s.canaries.GetActive(ctx, appID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNoActiveCanary
	}
	if err != nil {
		return nil, fmt.Errorf("canary: status: %w", err)
	}
	return c, nil
}

// SweepAutoPromote runs one round of the auto-promote/rollback loop. For
// each progressing canary whose window has elapsed and that opted into
// auto-promote, it probes health and either promotes (healthy) or aborts
// (unhealthy). Manual canaries (AutoPromote=false) are left untouched —
// they wait for an operator decision. Errors on one canary are logged
// and skipped so one bad app can't stall the rest. Exposed as a method
// so a background ticker (and tests) can drive it.
func (s *CanaryService) SweepAutoPromote(ctx context.Context) {
	if s.weighted == nil {
		return
	}
	canaries, err := s.canaries.ListProgressing(ctx)
	if err != nil {
		s.logger.Warn("canary sweep: list failed", "err", err)
		return
	}
	now := s.clock()
	for _, c := range canaries {
		if !c.AutoPromote || c.PromoteAfter == nil || now.Before(*c.PromoteAfter) {
			continue
		}
		// Bound each per-canary evaluation so a hung K8s Patch or wedged
		// kubectl child can't stall the whole sweep loop (and every other
		// progressing canary) indefinitely (PM26-07-05). Mirrors the
		// audit-retention sweep's per-pass timeout.
		evalCtx, cancel := context.WithTimeout(ctx, canaryEvalTimeout)
		s.evaluate(evalCtx, c)
		cancel()
	}
}

// canaryEvalTimeout bounds a single canary's probe + promote/abort in
// the sweep. Generous enough for a real K8s apply, short enough that
// one stuck canary doesn't hold the loop past the next tick.
const canaryEvalTimeout = 30 * time.Second

// defaultCanarySweepInterval is how often the auto-promote loop runs.
// Short enough that an auto-promote fires within a tick of its window
// closing; cheap because it only touches progressing canaries.
const defaultCanarySweepInterval = 15 * time.Second

// RunSweeper blocks until ctx is cancelled, running SweepAutoPromote on
// a ticker. Returns nil on a clean cancel — the signal the caller
// (server shutdown) waits for before declaring drain complete. Mirrors
// AppHealthChecker.Run. A nil weighted deployer makes every sweep a
// no-op, so wiring this unconditionally is harmless.
func (s *CanaryService) RunSweeper(ctx context.Context) error {
	s.SweepAutoPromote(ctx)
	t := time.NewTicker(defaultCanarySweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			s.SweepAutoPromote(ctx)
		}
	}
}

// evaluate probes one canary's health and promotes or aborts it. Used by
// the sweep; split out so the health decision is testable in isolation.
func (s *CanaryService) evaluate(ctx context.Context, c *model.AppCanary) {
	app, err := s.apps.Get(ctx, c.AppID)
	if err != nil {
		// App deleted out from under an in-flight canary: mark the row
		// aborted so the sweep stops re-evaluating it.
		s.logger.Warn("canary sweep: app gone, aborting canary", "app", c.AppID, "err", err)
		now := s.clock()
		c.Status = model.CanaryAborted
		c.Message = "app deleted during canary"
		c.ResolvedAt = &now
		_ = s.canaries.Update(ctx, c)
		return
	}
	healthy, msg := s.probeHealthy(ctx, app)
	if healthy {
		if _, err := s.Promote(ctx, c.AppID); err != nil {
			s.logger.Warn("canary sweep: auto-promote failed", "app", c.AppID, "err", err)
		}
		return
	}
	if _, err := s.Abort(ctx, c.AppID, "auto-rollback: "+msg); err != nil {
		s.logger.Warn("canary sweep: auto-rollback failed", "app", c.AppID, "err", err)
	}
}

// probeHealthy reports whether the canary workload is healthy. With no
// prober wired it assumes healthy (a target without a probe still
// auto-promotes after the window rather than hanging forever).
func (s *CanaryService) probeHealthy(ctx context.Context, app *model.App) (bool, string) {
	if s.prober == nil {
		return true, "no probe wired; assuming healthy"
	}
	health, msg, _ := s.prober.Probe(ctx, app)
	switch health {
	case model.AppHealthHealthy:
		return true, msg
	case model.AppHealthFailed:
		return false, msg
	default:
		// Unknown / degraded during the window is treated as not-yet-healthy;
		// the conservative choice is to roll back rather than promote a
		// workload we can't confirm is serving.
		return false, fmt.Sprintf("health %s: %s", health, msg)
	}
}

// loadActive fetches the app and its single progressing canary, mapping
// "none" to ErrNoActiveCanary. weighted is guaranteed non-nil here
// because a progressing canary can only exist after a successful Start,
// which requires a weighted deployer.
func (s *CanaryService) loadActive(ctx context.Context, appID string) (*model.App, *model.AppCanary, error) {
	c, err := s.canaries.GetActive(ctx, appID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, ErrNoActiveCanary
	}
	if err != nil {
		return nil, nil, fmt.Errorf("canary: load active: %w", err)
	}
	app, err := s.apps.Get(ctx, appID)
	if err != nil {
		return nil, nil, fmt.Errorf("canary: load app: %w", err)
	}
	return app, c, nil
}

// stableImageFor returns the image currently serving the app, used as
// the canary's rollback target (PM26-07-01). Two sources, newest wins:
// the most recently promoted canary's image (a promote means that image
// took 100% of traffic) and the most recent successful deploy's
// ImageRef (a plain deploy after a promote supersedes it). Returns ""
// when the app has no history — Start then splits the new image
// against itself. A richer impl would read the live Deployment; that
// remains a follow-up.
func (s *CanaryService) stableImageFor(ctx context.Context, app *model.App) string {
	var img string
	var at time.Time
	if prev, err := s.canaries.LatestPromoted(ctx, app.ID); err == nil && prev.CanaryImage != "" {
		img = prev.CanaryImage
		if prev.ResolvedAt != nil {
			at = *prev.ResolvedAt
		}
	}
	if s.deploys == nil {
		return img
	}
	// Newest-first; the first successful single-image row is what a plain
	// deploy last shipped. Compose deploys have an empty ImageRef and are
	// skipped (not rollback-eligible in v1).
	rows, err := s.deploys.ListByApp(ctx, app.ID, 10)
	if err != nil {
		s.logger.Warn("canary: stable-image deploy-history lookup failed", "app", app.ID, "err", err)
		return img
	}
	for _, d := range rows {
		if d.Status != model.RunStatusSuccess || d.ImageRef == "" {
			continue
		}
		if d.CreatedAt.After(at) {
			img = d.ImageRef
		}
		break
	}
	return img
}

// failPending flips the reserved (pending) row to a terminal failed
// state when the build or split could not be established. Updating the
// same row — rather than creating a second one — means a failed Start
// never orphans the one-per-app slot (the widened unique index would
// otherwise block the next Start). Best-effort: a write failure is
// logged, not surfaced (the caller already has the real error).
func (s *CanaryService) failPending(ctx context.Context, app *model.App, c *model.AppCanary, reason string) {
	now := s.clock()
	c.Status = model.CanaryFailed
	c.Weight = 0
	c.Healthy = false
	c.Message = "failed to establish canary: " + reason
	c.PromoteAfter = nil
	c.ResolvedAt = &now
	if err := s.canaries.Update(ctx, c); err != nil {
		s.logger.Warn("canary: record failed state", "app", app.ID, "err", err)
	}
	NotifyCanary(s.notifier, app, c, notifier.EventCanaryFailed, reason)
}
