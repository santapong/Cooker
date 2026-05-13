package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// defaultAppHealthInterval is how often the checker probes each App
// for readiness. Picked to be long enough that a fleet of 100 apps
// doesn't hammer the cluster API every few seconds, short enough that
// a freshly-deployed app shows green within a minute.
const defaultAppHealthInterval = 30 * time.Second

// AppLister is the narrow store contract the AppHealthChecker needs.
// Defining it locally lets tests inject a fake without touching the
// full store.AppStore surface.
type AppLister interface {
	List(ctx context.Context) ([]*model.App, error)
	// UpdateHealth writes the latest probe verdict. deployedURL may be
	// empty for targets that don't expose an ingress; an empty string
	// leaves a previously-written URL intact in the store.
	UpdateHealth(ctx context.Context, id string, status model.AppHealth, msg string, at time.Time, deployedURL string) error
}

// Prober probes a single App and returns its readiness verdict and
// the public ingress URL of the deployed workload (if the target
// exposes one). Implementations are per-target-kind; a registry maps
// DeployTarget kinds to their Prober. Return AppHealthUnknown + a
// short message when the target backend can't be reached or the app
// is missing upstream — never panic. Return an empty string for url
// when the target does not expose an ingress.
type Prober interface {
	Probe(ctx context.Context, app *model.App) (health model.AppHealth, msg string, url string)
}

// ProberFunc adapts a function value to the Prober interface so a
// constructor can register a closure without declaring a dedicated
// struct.
type ProberFunc func(ctx context.Context, app *model.App) (model.AppHealth, string, string)

// Probe implements Prober for ProberFunc.
func (f ProberFunc) Probe(ctx context.Context, app *model.App) (model.AppHealth, string, string) {
	return f(ctx, app)
}

// unknownProber is the catch-all for App targets without a registered
// probe (e.g. docker-host in dev, or a future kind that hasn't been
// wired). It writes "unknown" with a short hint so operators see why
// an App's health badge stays grey.
var unknownProber ProberFunc = func(_ context.Context, app *model.App) (model.AppHealth, string, string) {
	return model.AppHealthUnknown, "no health probe registered for target kind " + string(app.DeployTarget.Kind), ""
}

// AppHealthChecker is a periodic background service. On each tick it
// lists every App, dispatches a per-target Prober, and writes the
// verdict via AppLister.UpdateHealth. The checker is designed for
// SIGTERM-clean drain: Run blocks until ctx is cancelled, and the
// caller is expected to wait for Run to return before declaring
// shutdown complete.
type AppHealthChecker struct {
	apps     AppLister
	probers  map[model.DeployTargetKind]Prober
	interval time.Duration
	clock    func() time.Time
	logger   *slog.Logger
}

// AppHealthOption configures a new AppHealthChecker.
type AppHealthOption func(*AppHealthChecker)

// WithAppHealthInterval overrides the default tick interval. Values
// <= 0 are ignored so callers can pass an unset config field through.
func WithAppHealthInterval(d time.Duration) AppHealthOption {
	return func(c *AppHealthChecker) {
		if d > 0 {
			c.interval = d
		}
	}
}

// WithProber registers a Prober for a specific deploy-target kind.
// Multiple WithProber options compose; later registrations override
// earlier ones for the same kind. Pass nil to remove a prior
// registration (the default unknownProber is restored automatically
// at Probe time).
func WithProber(kind model.DeployTargetKind, p Prober) AppHealthOption {
	return func(c *AppHealthChecker) {
		if c.probers == nil {
			c.probers = map[model.DeployTargetKind]Prober{}
		}
		if p == nil {
			delete(c.probers, kind)
			return
		}
		c.probers[kind] = p
	}
}

// WithClock injects a deterministic time source for tests. Production
// uses time.Now.
func WithClock(now func() time.Time) AppHealthOption {
	return func(c *AppHealthChecker) {
		if now != nil {
			c.clock = now
		}
	}
}

// NewAppHealthChecker constructs a checker with sensible defaults.
// Without any WithProber options every App reports "unknown"; that's
// a safe baseline that callers grow into by registering real probes
// per target kind as they're wired.
func NewAppHealthChecker(apps AppLister, opts ...AppHealthOption) *AppHealthChecker {
	c := &AppHealthChecker{
		apps:     apps,
		interval: defaultAppHealthInterval,
		clock:    time.Now,
		logger:   slog.Default(),
		probers:  map[model.DeployTargetKind]Prober{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// proberFor returns the registered Prober for kind, or unknownProber.
func (c *AppHealthChecker) proberFor(kind model.DeployTargetKind) Prober {
	if p, ok := c.probers[kind]; ok && p != nil {
		return p
	}
	return unknownProber
}

// tick runs one round of probes. Exposed as a method so tests can
// drive it deterministically without sleeping. Errors from any one
// probe or store write are logged and skipped; one bad app must not
// block the rest. Each per-app probe call is wrapped in a recover so
// a panicking cloud-SDK implementation cannot kill the checker goroutine.
func (c *AppHealthChecker) tick(ctx context.Context) {
	apps, err := c.apps.List(ctx)
	if err != nil {
		c.logger.Warn("app health: list apps failed", "err", err)
		return
	}
	now := c.clock()
	for _, a := range apps {
		if a == nil {
			continue
		}
		func(app *model.App) {
			defer func() {
				if r := recover(); r != nil {
					c.logger.Warn("app health: probe panicked",
						"app", app.ID, "kind", app.DeployTarget.Kind, "panic", r)
					_ = c.apps.UpdateHealth(ctx, app.ID,
						model.AppHealthUnknown, "probe panicked", c.clock(), "")
				}
			}()
			status, msg, deployedURL := c.proberFor(app.DeployTarget.Kind).Probe(ctx, app)
			if err := c.apps.UpdateHealth(ctx, app.ID, status, msg, now, deployedURL); err != nil {
				// ErrNotFound means the app was deleted between List and
				// UpdateHealth — a benign race, log at debug.
				if errors.Is(err, store.ErrNotFound) {
					c.logger.Debug("app health: app deleted before write", "app", app.ID)
					return
				}
				c.logger.Warn("app health: write failed", "app", app.ID, "err", err)
			}
		}(a)
	}
}

// Run blocks until ctx is cancelled, ticking the checker every
// c.interval. Returns nil on a clean cancel — that's the signal the
// caller (server shutdown) waits for before declaring drain complete.
func (c *AppHealthChecker) Run(ctx context.Context) error {
	// Run one tick immediately so apps don't have to wait an interval
	// for their first probe after boot.
	c.tick(ctx)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			c.tick(ctx)
		}
	}
}
