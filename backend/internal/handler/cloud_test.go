package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
)

// fakeCloudSvc implements CloudInventoryService for the handler tests.
type fakeCloudSvc struct {
	enabled      bool
	inv          model.CloudInventory
	refreshCalls int
}

func (f *fakeCloudSvc) Enabled() bool                                    { return f.enabled }
func (f *fakeCloudSvc) Inventory(_ context.Context) model.CloudInventory { return f.inv }
func (f *fakeCloudSvc) Refresh(_ context.Context) model.CloudInventory {
	f.refreshCalls++
	return f.inv
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
}

func TestGetCloudInventory_NilServiceEnabledFalse(t *testing.T) {
	r, _ := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/cloud/inventory", h.GetCloudInventory)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/cloud/inventory", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (not error) when unconfigured, got %d: %s", w.Code, w.Body.String())
	}
	var got model.CloudInventory
	decodeJSON(t, w, &got)
	if got.Enabled {
		t.Errorf("expected enabled=false, got true")
	}
	// Slices must serialise as [] not null, so the TS type is stable.
	if got.Providers == nil || got.Results == nil {
		t.Errorf("expected empty (non-null) slices, got providers=%v results=%v", got.Providers, got.Results)
	}
}

func TestGetCloudInventory_DisabledServiceEnabledFalse(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/cloud/inventory", h.GetCloudInventory)
	})
	h.CloudInventory = &fakeCloudSvc{enabled: false}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/cloud/inventory", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got model.CloudInventory
	decodeJSON(t, w, &got)
	if got.Enabled {
		t.Errorf("expected enabled=false from a provider-less service")
	}
}

func TestGetCloudInventory_EnabledReturnsAggregate(t *testing.T) {
	inv := model.CloudInventory{
		Enabled:   true,
		Providers: []model.CloudProvider{model.CloudProviderAWS, model.CloudProviderGCP},
		Results: []model.ProviderInventory{
			{
				Provider:  model.CloudProviderAWS,
				Resources: []model.CloudResource{{Provider: model.CloudProviderAWS, Type: model.CloudResourceCompute, ID: "i-1"}},
				Cost:      &model.CostSummary{Provider: model.CloudProviderAWS, Total: "10.00", Currency: "USD"},
			},
			{
				Provider: model.CloudProviderGCP,
				Error:    "gcp: compute: permission denied",
			},
		},
		FetchedAt: "2026-06-12T00:00:00Z",
	}
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/cloud/inventory", h.GetCloudInventory)
	})
	h.CloudInventory = &fakeCloudSvc{enabled: true, inv: inv}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/cloud/inventory", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got model.CloudInventory
	decodeJSON(t, w, &got)
	if !got.Enabled || len(got.Results) != 2 {
		t.Fatalf("expected enabled with 2 results, got %+v", got)
	}
	// Partial failure surfaces per-provider, not as an HTTP error.
	var gcp *model.ProviderInventory
	for i := range got.Results {
		if got.Results[i].Provider == model.CloudProviderGCP {
			gcp = &got.Results[i]
		}
	}
	if gcp == nil || gcp.Error == "" {
		t.Errorf("expected the GCP provider to carry its error inline, got %+v", got.Results)
	}
}

func TestGetCloudCosts_ProjectsCostPortion(t *testing.T) {
	inv := model.CloudInventory{
		Enabled:   true,
		Providers: []model.CloudProvider{model.CloudProviderAWS, model.CloudProviderGCP},
		Results: []model.ProviderInventory{
			{Provider: model.CloudProviderAWS, Cost: &model.CostSummary{Provider: model.CloudProviderAWS, Total: "10.00", Currency: "USD"}},
			{Provider: model.CloudProviderGCP, Error: "boom"},
		},
		FetchedAt: "2026-06-12T00:00:00Z",
	}
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/cloud/costs", h.GetCloudCosts)
	})
	h.CloudInventory = &fakeCloudSvc{enabled: true, inv: inv}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/cloud/costs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got cloudCostsResponse
	decodeJSON(t, w, &got)
	if !got.Enabled || len(got.Costs) != 2 {
		t.Fatalf("expected 2 cost entries, got %+v", got)
	}
	var aws, gcp *cloudProviderCost
	for i := range got.Costs {
		switch got.Costs[i].Provider {
		case model.CloudProviderAWS:
			aws = &got.Costs[i]
		case model.CloudProviderGCP:
			gcp = &got.Costs[i]
		}
	}
	if aws == nil || aws.Cost == nil || aws.Cost.Total != "10.00" {
		t.Errorf("expected AWS cost projected, got %+v", aws)
	}
	if gcp == nil || gcp.Error == "" || gcp.Cost != nil {
		t.Errorf("expected GCP error projected with nil cost, got %+v", gcp)
	}
}

func TestGetCloudCosts_NilServiceEnabledFalse(t *testing.T) {
	r, _ := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/cloud/costs", h.GetCloudCosts)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/cloud/costs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got cloudCostsResponse
	decodeJSON(t, w, &got)
	if got.Enabled {
		t.Errorf("expected enabled=false")
	}
	if got.Costs == nil || got.Providers == nil {
		t.Errorf("expected empty (non-null) slices")
	}
}

func TestRefreshCloud_BustsCacheWhenEnabled(t *testing.T) {
	fake := &fakeCloudSvc{enabled: true, inv: model.CloudInventory{Enabled: true}}
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.POST("/cloud/refresh", h.RefreshCloud)
	})
	h.CloudInventory = fake
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/cloud/refresh", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if fake.refreshCalls != 1 {
		t.Errorf("expected Refresh to be called once, got %d", fake.refreshCalls)
	}
}

func TestRefreshCloud_NilServiceNoop(t *testing.T) {
	r, _ := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.POST("/cloud/refresh", h.RefreshCloud)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/cloud/refresh", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (harmless no-op), got %d", w.Code)
	}
	var got model.CloudInventory
	decodeJSON(t, w, &got)
	if got.Enabled {
		t.Errorf("expected enabled=false on unconfigured refresh")
	}
}
