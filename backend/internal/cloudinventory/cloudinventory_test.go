package cloudinventory

import (
	"context"
	"errors"
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
