// Package cloudinventory provides a read-only, cached view of cloud
// resources and month-to-date cost across one or more providers (AWS,
// GCP). It mirrors the internal/kube package's posture: strictly
// read-only (list/describe/cost APIs only — never a mutation), lazily
// constructed so a server with no cloud configured still boots, and
// nil-safe at the handler boundary.
//
// The Service fans out to every enabled Provider concurrently,
// aggregates the results, and caches the aggregate in memory with a TTL.
// Per-provider failures are isolated: one cloud being unreachable yields
// partial results with a per-provider error string, never a whole-
// request failure.
//
// Credentials never appear on any returned type and are never logged.
package cloudinventory

import (
	"context"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// Provider is the narrow read-only surface each cloud implements. All
// methods must be side-effect-free against the cloud (list/describe/cost
// only). Implementations live under aws/ and gcp/.
type Provider interface {
	// Name reports which cloud this provider represents.
	Name() model.CloudProvider
	// ListResources returns the discovered compute instances, managed
	// Kubernetes clusters, and container registries for the configured
	// account/project. It must not mutate any cloud resource.
	ListResources(ctx context.Context) ([]model.CloudResource, error)
	// CostSummary returns current month-to-date spend grouped by
	// service. Implementations that cannot compute cost should return a
	// zero CostSummary and a nil error rather than failing the whole
	// provider.
	CostSummary(ctx context.Context) (model.CostSummary, error)
}

// DefaultTTL is the cache lifetime used when none is configured. Cloud
// list/cost APIs are rate-limited and (for Cost Explorer) billed
// per-request, so the panel reads from cache by default and only hits
// the providers on expiry or an explicit refresh.
const DefaultTTL = 5 * time.Minute

// fetchTimeout bounds a single fan-out across all providers so one slow
// cloud can't pin a request (or the refresh path) open indefinitely. It
// is generous because Cost Explorer in particular can be slow.
const fetchTimeout = 30 * time.Second

// Service aggregates one or more providers behind a TTL cache. Construct
// with New. A Service with zero providers is valid and reports
// Enabled=false from Inventory — the dev/unconfigured path.
type Service struct {
	providers []Provider
	ttl       time.Duration
	// now is injected for tests; defaults to time.Now.
	now func() time.Time

	mu     sync.Mutex
	cache  *model.CloudInventory
	expiry time.Time
}

// Option configures a Service.
type Option func(*Service)

// WithTTL overrides the cache lifetime. A non-positive value is ignored
// (DefaultTTL stands), so a misconfigured COOKER_CLOUD_CACHE_TTL can't
// disable caching and hammer the paid cost APIs.
func WithTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.ttl = ttl
		}
	}
}

// withClock injects a clock for tests.
func withClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// New constructs a Service over the given providers. Providers may be
// empty (the unconfigured path); the resulting Service reports
// Enabled=false. nil providers in the slice are dropped defensively.
func New(providers []Provider, opts ...Option) *Service {
	s := &Service{
		ttl: DefaultTTL,
		now: time.Now,
	}
	for _, p := range providers {
		if p != nil {
			s.providers = append(s.providers, p)
		}
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Enabled reports whether any provider is configured.
func (s *Service) Enabled() bool { return s != nil && len(s.providers) > 0 }

// providerNames returns the configured provider names in registration
// order (stable for the UI). Cheap; recomputed rather than cached.
func (s *Service) providerNames() []model.CloudProvider {
	names := make([]model.CloudProvider, 0, len(s.providers))
	for _, p := range s.providers {
		names = append(names, p.Name())
	}
	return names
}

// Inventory returns the aggregated inventory, served from cache when a
// fresh snapshot exists. On a cache miss (or expiry) it fans out to all
// providers, caches the result, and returns it. A nil or
// provider-less Service returns an Enabled=false snapshot without
// touching any cloud.
func (s *Service) Inventory(ctx context.Context) model.CloudInventory {
	if !s.Enabled() {
		return model.CloudInventory{
			Enabled:   false,
			Providers: []model.CloudProvider{},
			Results:   []model.ProviderInventory{},
			FetchedAt: s.now().UTC().Format(time.RFC3339),
		}
	}

	s.mu.Lock()
	if s.cache != nil && s.now().Before(s.expiry) {
		cached := *s.cache
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()

	inv := s.fetch(ctx)

	s.mu.Lock()
	s.cache = &inv
	s.expiry = s.now().Add(s.ttl)
	s.mu.Unlock()

	return inv
}

// Refresh busts the cache and re-fetches synchronously, returning the
// fresh snapshot. Backs POST /cloud/refresh.
func (s *Service) Refresh(ctx context.Context) model.CloudInventory {
	s.mu.Lock()
	s.cache = nil
	s.expiry = time.Time{}
	s.mu.Unlock()
	return s.Inventory(ctx)
}

// fetch fans out to every provider concurrently and aggregates. Each
// provider's resources and cost are gathered under one bounded context;
// a provider error is recorded on that provider's ProviderInventory.Error
// and does NOT fail the aggregate (partial-failure isolation).
func (s *Service) fetch(ctx context.Context) model.CloudInventory {
	fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	results := make([]model.ProviderInventory, len(s.providers))
	var wg sync.WaitGroup
	for i, p := range s.providers {
		wg.Add(1)
		go func(i int, p Provider) {
			defer wg.Done()
			results[i] = fetchOne(fctx, p)
		}(i, p)
	}
	wg.Wait()

	return model.CloudInventory{
		Enabled:   true,
		Providers: s.providerNames(),
		Results:   results,
		FetchedAt: s.now().UTC().Format(time.RFC3339),
	}
}

// fetchOne queries a single provider. The first error (resources OR
// cost) is surfaced as the provider's Error and the partial data is
// dropped for that provider — we prefer a clean "this provider failed"
// banner over a half-populated table that looks authoritative.
func fetchOne(ctx context.Context, p Provider) model.ProviderInventory {
	out := model.ProviderInventory{Provider: p.Name()}

	resources, err := p.ListResources(ctx)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	cost, err := p.CostSummary(ctx)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Resources = resources
	if cost.Provider == "" {
		cost.Provider = p.Name()
	}
	out.Cost = &cost
	return out
}
