package cloudinventory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// fakeProvider is a test double recording call counts and returning
// canned data/errors. callDelay simulates a slow cloud to exercise the
// concurrent fan-out.
type fakeProvider struct {
	name      model.CloudProvider
	resources []model.CloudResource
	cost      model.CostSummary
	listErr   error
	costErr   error
	callDelay time.Duration

	listCalls atomic.Int32
	costCalls atomic.Int32
}

func (f *fakeProvider) Name() model.CloudProvider { return f.name }

func (f *fakeProvider) ListResources(ctx context.Context) ([]model.CloudResource, error) {
	f.listCalls.Add(1)
	if f.callDelay > 0 {
		select {
		case <-time.After(f.callDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.resources, nil
}

func (f *fakeProvider) CostSummary(ctx context.Context) (model.CostSummary, error) {
	f.costCalls.Add(1)
	if f.costErr != nil {
		return model.CostSummary{}, f.costErr
	}
	return f.cost, nil
}

func TestService_DisabledWhenNoProviders(t *testing.T) {
	s := New(nil)
	if s.Enabled() {
		t.Fatal("expected Enabled()=false with no providers")
	}
	inv := s.Inventory(context.Background())
	if inv.Enabled {
		t.Errorf("expected inventory Enabled=false, got true")
	}
	if len(inv.Results) != 0 || len(inv.Providers) != 0 {
		t.Errorf("expected empty results/providers, got %+v", inv)
	}
	if inv.FetchedAt == "" {
		t.Errorf("expected FetchedAt to be set even when disabled")
	}
}

func TestService_NilProvidersDropped(t *testing.T) {
	// A nil interface value in the slice must not be retained (would
	// panic on Name()).
	s := New([]Provider{nil, &fakeProvider{name: model.CloudProviderAWS}, nil})
	if got := len(s.providers); got != 1 {
		t.Fatalf("expected 1 retained provider, got %d", got)
	}
}

func TestService_AggregatesAcrossProviders(t *testing.T) {
	aws := &fakeProvider{
		name: model.CloudProviderAWS,
		resources: []model.CloudResource{
			{Provider: model.CloudProviderAWS, Type: model.CloudResourceCompute, ID: "i-1"},
		},
		cost: model.CostSummary{Provider: model.CloudProviderAWS, Total: "10.00", Currency: "USD"},
	}
	gcp := &fakeProvider{
		name: model.CloudProviderGCP,
		resources: []model.CloudResource{
			{Provider: model.CloudProviderGCP, Type: model.CloudResourceCluster, ID: "gke-1"},
		},
		cost: model.CostSummary{Provider: model.CloudProviderGCP, Total: "5.00", Currency: "USD"},
	}
	s := New([]Provider{aws, gcp})
	inv := s.Inventory(context.Background())

	if !inv.Enabled {
		t.Fatal("expected Enabled=true")
	}
	if len(inv.Results) != 2 {
		t.Fatalf("expected 2 provider results, got %d", len(inv.Results))
	}
	if len(inv.Providers) != 2 || inv.Providers[0] != model.CloudProviderAWS || inv.Providers[1] != model.CloudProviderGCP {
		t.Errorf("expected providers [aws gcp] in registration order, got %v", inv.Providers)
	}
	for _, r := range inv.Results {
		if r.Error != "" {
			t.Errorf("provider %s unexpectedly errored: %s", r.Provider, r.Error)
		}
		if r.Cost == nil {
			t.Errorf("provider %s missing cost", r.Provider)
		}
	}
}

func TestService_PartialFailureIsolation(t *testing.T) {
	good := &fakeProvider{
		name:      model.CloudProviderAWS,
		resources: []model.CloudResource{{ID: "i-1"}},
		cost:      model.CostSummary{Total: "1.00"},
	}
	bad := &fakeProvider{
		name:    model.CloudProviderGCP,
		listErr: errors.New("gcp: compute: permission denied"),
	}
	s := New([]Provider{good, bad})
	inv := s.Inventory(context.Background())

	if !inv.Enabled {
		t.Fatal("expected Enabled=true even with one provider failing")
	}
	var awsRes, gcpRes *model.ProviderInventory
	for i := range inv.Results {
		switch inv.Results[i].Provider {
		case model.CloudProviderAWS:
			awsRes = &inv.Results[i]
		case model.CloudProviderGCP:
			gcpRes = &inv.Results[i]
		}
	}
	if awsRes == nil || gcpRes == nil {
		t.Fatalf("expected both providers present, got %+v", inv.Results)
	}
	if awsRes.Error != "" {
		t.Errorf("healthy provider should not carry an error, got %q", awsRes.Error)
	}
	if len(awsRes.Resources) != 1 {
		t.Errorf("healthy provider should still return its resources, got %d", len(awsRes.Resources))
	}
	if gcpRes.Error == "" {
		t.Errorf("failing provider must carry a non-empty Error")
	}
	if len(gcpRes.Resources) != 0 {
		t.Errorf("failing provider must not return partial resources, got %d", len(gcpRes.Resources))
	}
	// The cost error path is also isolated.
	if gcpRes.Cost != nil {
		t.Errorf("failing provider must not carry a cost summary")
	}
}

func TestService_CostErrorIsolatesProvider(t *testing.T) {
	p := &fakeProvider{
		name:      model.CloudProviderAWS,
		resources: []model.CloudResource{{ID: "i-1"}},
		costErr:   errors.New("aws: costexplorer: throttled"),
	}
	s := New([]Provider{p})
	inv := s.Inventory(context.Background())
	if len(inv.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(inv.Results))
	}
	if inv.Results[0].Error == "" {
		t.Errorf("a cost error must surface as the provider Error")
	}
}

func TestService_CachesUntilTTLExpiry(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	p := &fakeProvider{
		name:      model.CloudProviderAWS,
		resources: []model.CloudResource{{ID: "i-1"}},
	}
	s := New([]Provider{p}, WithTTL(time.Minute), withClock(clock.Now))

	s.Inventory(context.Background())
	s.Inventory(context.Background())
	if got := p.listCalls.Load(); got != 1 {
		t.Fatalf("expected 1 provider call within TTL (cache hit), got %d", got)
	}

	// Advance past the TTL: the next call must re-fetch.
	clock.Advance(61 * time.Second)
	s.Inventory(context.Background())
	if got := p.listCalls.Load(); got != 2 {
		t.Fatalf("expected 2 provider calls after TTL expiry, got %d", got)
	}
}

func TestService_RefreshBustsCache(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	p := &fakeProvider{name: model.CloudProviderAWS}
	s := New([]Provider{p}, WithTTL(time.Hour), withClock(clock.Now))

	s.Inventory(context.Background())
	if got := p.listCalls.Load(); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}
	// Refresh inside the TTL must still re-fetch.
	s.Refresh(context.Background())
	if got := p.listCalls.Load(); got != 2 {
		t.Fatalf("expected Refresh to bust the cache (2 calls), got %d", got)
	}
}

func TestService_ConcurrentFanOut(t *testing.T) {
	// Two providers each delaying 100ms; a sequential implementation
	// would take ~200ms. Assert the fan-out completes well under that.
	a := &fakeProvider{name: model.CloudProviderAWS, callDelay: 100 * time.Millisecond}
	b := &fakeProvider{name: model.CloudProviderGCP, callDelay: 100 * time.Millisecond}
	s := New([]Provider{a, b})

	start := time.Now()
	inv := s.Inventory(context.Background())
	elapsed := time.Since(start)

	if len(inv.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(inv.Results))
	}
	if elapsed > 180*time.Millisecond {
		t.Errorf("fan-out took %v; expected concurrent (< 180ms), providers may be running sequentially", elapsed)
	}
}

func TestService_WithTTLIgnoresNonPositive(t *testing.T) {
	p := &fakeProvider{name: model.CloudProviderAWS}
	s := New([]Provider{p}, WithTTL(0), WithTTL(-time.Second))
	if s.ttl != DefaultTTL {
		t.Errorf("expected non-positive TTL to be ignored (DefaultTTL), got %v", s.ttl)
	}
}

// fakeClock is a controllable monotonic clock for cache-TTL tests.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// PM26-07-08: concurrent cache-miss reads share ONE fan-out (singleflight),
// not N billed calls to Cost Explorer.
func TestService_ConcurrentMissesSingleFlight(t *testing.T) {
	p := &fakeProvider{
		name:      model.CloudProviderAWS,
		resources: []model.CloudResource{{ID: "i-1"}},
		callDelay: 50 * time.Millisecond, // hold the fan-out open so readers pile up
	}
	s := New([]Provider{p})

	const readers = 8
	var wg sync.WaitGroup
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() { defer wg.Done(); _ = s.Inventory(context.Background()) }()
	}
	wg.Wait()

	if got := p.listCalls.Load(); got != 1 {
		t.Errorf("expected 1 shared fan-out for %d concurrent cache-miss reads, got %d billed calls", readers, got)
	}
}

// gatedProvider blocks in ListResources until release is closed, so a
// test can hold a fetch in-flight while another completes.
type gatedProvider struct {
	name    model.CloudProvider
	id      string
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (g *gatedProvider) Name() model.CloudProvider { return g.name }
func (g *gatedProvider) ListResources(ctx context.Context) ([]model.CloudResource, error) {
	g.calls.Add(1)
	if g.started != nil {
		close(g.started)
	}
	if g.release != nil {
		<-g.release
	}
	return []model.CloudResource{{ID: g.id}}, nil
}
func (g *gatedProvider) CostSummary(context.Context) (model.CostSummary, error) {
	return model.CostSummary{}, nil
}

// PM26-07-09: a slow in-flight fetch must not overwrite a newer Refresh
// snapshot when it finally completes.
func TestService_StaleFetchDoesNotClobberRefresh(t *testing.T) {
	slow := &gatedProvider{name: model.CloudProviderAWS, id: "stale", started: make(chan struct{}), release: make(chan struct{})}
	s := New([]Provider{slow})

	// Leader fetch starts and blocks inside the provider.
	done := make(chan model.CloudInventory, 1)
	go func() { done <- s.Inventory(context.Background()) }()
	<-slow.started

	// While it's blocked, swap in a fast provider and Refresh — this bumps
	// the generation and writes the fresh snapshot.
	fresh := &fakeProvider{name: model.CloudProviderAWS, resources: []model.CloudResource{{ID: "fresh"}}}
	s.mu.Lock()
	s.providers = []Provider{fresh}
	s.mu.Unlock()
	refreshed := s.Refresh(context.Background())
	if len(refreshed.Results[0].Resources) == 0 || refreshed.Results[0].Resources[0].ID != "fresh" {
		t.Fatalf("refresh should return fresh data, got %+v", refreshed.Results)
	}

	// Now let the stale leader finish. Its (stale) result must NOT become
	// the cache.
	close(slow.release)
	<-done

	cached := s.Inventory(context.Background()) // served from cache (fresh)
	if cached.Results[0].Resources[0].ID != "fresh" {
		t.Errorf("stale in-flight fetch clobbered the newer refresh snapshot: %+v", cached.Results)
	}
}

// PM26-07-12: a totally-failed fetch is cached only briefly (negativeTTL),
// not for the full TTL, so it retries soon.
func TestService_FailedFetchNegativeTTL(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	p := &fakeProvider{name: model.CloudProviderAWS, listErr: errors.New("aws: throttled")}
	s := New([]Provider{p}, WithTTL(time.Hour), withClock(clock.Now))

	s.Inventory(context.Background()) // all-failed → cached with negativeTTL
	if got := p.listCalls.Load(); got != 1 {
		t.Fatalf("precondition: 1 call, got %d", got)
	}
	// Within the negative TTL: still cached, no re-fetch.
	clock.Advance(20 * time.Second)
	s.Inventory(context.Background())
	if got := p.listCalls.Load(); got != 1 {
		t.Errorf("within negativeTTL the failed snapshot should be cached, got %d calls", got)
	}
	// Past the negative TTL (but well within the full 1h TTL): re-fetch.
	clock.Advance(15 * time.Second)
	s.Inventory(context.Background())
	if got := p.listCalls.Load(); got != 2 {
		t.Errorf("past negativeTTL a failed snapshot must re-fetch, got %d calls (a good snapshot would still be cached for 1h)", got)
	}
}
