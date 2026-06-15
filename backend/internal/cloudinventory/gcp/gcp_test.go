package gcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"

	"github.com/santapong/cooker/internal/model"
)

func constLister(rs []model.CloudResource) listFunc {
	return func(_ context.Context) ([]model.CloudResource, error) { return rs, nil }
}

func errLister(err error) listFunc {
	return func(_ context.Context) ([]model.CloudResource, error) { return nil, err }
}

func TestListResources_AggregatesAndSortsByName(t *testing.T) {
	p := newWithListers("proj",
		constLister([]model.CloudResource{
			{Provider: model.CloudProviderGCP, Type: model.CloudResourceCompute, ID: "9", Name: "web-2"},
			{Provider: model.CloudProviderGCP, Type: model.CloudResourceCompute, ID: "8", Name: "web-1"},
		}),
		constLister([]model.CloudResource{
			{Provider: model.CloudProviderGCP, Type: model.CloudResourceCluster, ID: "gke-a", Name: "gke-a"},
		}),
		constLister([]model.CloudResource{
			{Provider: model.CloudProviderGCP, Type: model.CloudResourceRegistry, ID: "p/l/r/cooker", Name: "cooker"},
		}),
		nil,
	)
	got, err := p.ListResources(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 resources, got %d", len(got))
	}
	// Deterministic ascending-by-name ordering.
	wantOrder := []string{"cooker", "gke-a", "web-1", "web-2"}
	for i, w := range wantOrder {
		if got[i].Name != w {
			t.Errorf("position %d: expected %q, got %q (order: %+v)", i, w, got[i].Name, names(got))
		}
	}
}

func TestListResources_PropagatesError(t *testing.T) {
	p := newWithListers("proj",
		constLister(nil),
		errLister(errors.New("gcp: gke list clusters: 403")),
		constLister(nil),
		nil,
	)
	if _, err := p.ListResources(context.Background()); err == nil {
		t.Fatal("expected the cluster lister error to propagate")
	}
}

func TestName(t *testing.T) {
	p := newWithListers("proj", constLister(nil), constLister(nil), constLister(nil), nil)
	if p.Name() != model.CloudProviderGCP {
		t.Errorf("expected provider gcp, got %s", p.Name())
	}
}

func TestCostSummary_ReturnsLabelledZero(t *testing.T) {
	p := newWithListers("proj", constLister(nil), constLister(nil), constLister(nil),
		func() time.Time { return time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC) })
	cs, err := p.CostSummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cs.Provider != model.CloudProviderGCP {
		t.Errorf("expected provider gcp, got %s", cs.Provider)
	}
	if cs.Total != "0.00" {
		t.Errorf("expected zero total (GCP cost export not wired), got %s", cs.Total)
	}
	if cs.Start != "2026-06-01" || cs.End != "2026-06-12" {
		t.Errorf("expected MTD window 2026-06-01..2026-06-12, got %s..%s", cs.Start, cs.End)
	}
	if len(cs.Services) != 0 {
		t.Errorf("expected no service lines, got %d", len(cs.Services))
	}
}

func TestNew_RequiresProjectID(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("expected error when ProjectID is empty")
	}
}

// TestComputeLister_AgainstFakeEndpoint drives a REAL compute REST client
// through an httptest server (no real GCP, no credentials), proving the
// SDK-backed instance lister parses an aggregatedList response into
// CloudResources. This is the SDK-endpoint-override path the task asks
// for.
func TestComputeLister_AgainstFakeEndpoint(t *testing.T) {
	const body = `{
      "items": {
        "zones/us-central1-a": {
          "instances": [
            {"id": "111", "name": "vm-a", "status": "RUNNING",
             "zone": "https://www.googleapis.com/compute/v1/projects/proj/zones/us-central1-a",
             "labels": {"team": "platform"}}
          ]
        },
        "zones/europe-west1-b": {
          "instances": [
            {"id": "222", "name": "vm-b", "status": "TERMINATED",
             "zone": "https://www.googleapis.com/compute/v1/projects/proj/zones/europe-west1-b"}
          ]
        }
      }
    }`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	ctx := context.Background()
	svc, err := compute.NewService(ctx,
		option.WithoutAuthentication(),
		option.WithEndpoint(srv.URL),
	)
	if err != nil {
		t.Fatalf("compute.NewService: %v", err)
	}
	lister := computeInstancesLister(svc, "proj")
	got, err := lister(ctx)
	if err != nil {
		t.Fatalf("lister error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 instances from the fake endpoint, got %d: %+v", len(got), got)
	}
	byName := map[string]model.CloudResource{}
	for _, r := range got {
		byName[r.Name] = r
		if r.Type != model.CloudResourceCompute || r.Provider != model.CloudProviderGCP {
			t.Errorf("resource %q has wrong type/provider: %+v", r.Name, r)
		}
	}
	if a := byName["vm-a"]; a.ID != "111" || a.Status != "RUNNING" || a.Region != "us-central1-a" || a.Tags["team"] != "platform" {
		t.Errorf("vm-a parsed wrong: %+v", a)
	}
	if b := byName["vm-b"]; b.ID != "222" || b.Status != "TERMINATED" || b.Region != "europe-west1-b" {
		t.Errorf("vm-b parsed wrong: %+v", b)
	}
}

// TestComputeLister_EndpointError confirms a non-2xx from the endpoint
// surfaces as a wrapped error (not a silent empty list).
func TestComputeLister_EndpointError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":403,"message":"denied"}}`, http.StatusForbidden)
	}))
	defer srv.Close()

	ctx := context.Background()
	svc, err := compute.NewService(ctx, option.WithoutAuthentication(), option.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("compute.NewService: %v", err)
	}
	if _, err := computeInstancesLister(svc, "proj")(ctx); err == nil {
		t.Fatal("expected an error from a 403 endpoint")
	}
}

func TestHelpers(t *testing.T) {
	if got := lastPathSegment("projects/p/zones/us-central1-a"); got != "us-central1-a" {
		t.Errorf("lastPathSegment: got %q", got)
	}
	if got := lastPathSegment("bare"); got != "bare" {
		t.Errorf("lastPathSegment bare: got %q", got)
	}
	if got := artifactRepoLocation("projects/p/locations/asia-east1/repositories/r"); got != "asia-east1" {
		t.Errorf("artifactRepoLocation: got %q", got)
	}
	if nonEmptyLabels(map[string]string{}) != nil {
		t.Error("expected empty labels to map to nil")
	}
}

func names(rs []model.CloudResource) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}
