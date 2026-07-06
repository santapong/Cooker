package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
)

// CloudInventoryService is the slice of cloudinventory.Service the
// handlers need. Defining it as an interface here keeps the handler
// package free of the concrete service's transitive cloud-SDK imports
// and lets tests inject a fake.
type CloudInventoryService interface {
	Enabled() bool
	Inventory(ctx context.Context) model.CloudInventory
	Refresh(ctx context.Context) model.CloudInventory
}

// emptyCloudInventory is the 200 response used when no provider is
// configured (or the service wasn't wired): enabled=false with empty
// slices, so the UI renders a friendly "configure a cloud" state rather
// than an error. The fields mirror model.CloudInventory's zero-with-
// empty-slices shape so the frontend type is stable.
func emptyCloudInventory() model.CloudInventory {
	return model.CloudInventory{
		Enabled:   false,
		Providers: []model.CloudProvider{},
		Results:   []model.ProviderInventory{},
	}
}

// GetCloudInventory returns the aggregated cloud resource inventory
// across enabled providers, served from the service's TTL cache. When
// no provider is configured it returns 200 with enabled=false (not an
// error), so the panel can show an empty state.
func (h *Handler) GetCloudInventory(c *gin.Context) {
	if h.CloudInventory == nil || !h.CloudInventory.Enabled() {
		c.JSON(http.StatusOK, emptyCloudInventory())
		return
	}
	c.JSON(http.StatusOK, h.CloudInventory.Inventory(c.Request.Context()))
}

// cloudCostsResponse is the shape of GET /cloud/costs: the per-provider
// cost summaries projected out of the aggregated inventory, plus the
// same enabled / errors signalling so the UI can render partial
// failures consistently with the inventory view.
type cloudCostsResponse struct {
	Enabled   bool                  `json:"enabled"`
	Providers []model.CloudProvider `json:"providers"`
	Costs     []cloudProviderCost   `json:"costs"`
	FetchedAt string                `json:"fetchedAt"`
}

// cloudProviderCost is one provider's cost summary (or its error). Cost
// is nil when the provider errored; Error is empty otherwise.
type cloudProviderCost struct {
	Provider model.CloudProvider `json:"provider"`
	Cost     *model.CostSummary  `json:"cost,omitempty"`
	Error    string              `json:"error,omitempty"`
}

// GetCloudCosts returns month-to-date spend per provider. It reads the
// same cached snapshot as GetCloudInventory and projects the cost
// portion, so the two views never disagree and a single fan-out backs
// both. No provider configured -> 200 with enabled=false.
func (h *Handler) GetCloudCosts(c *gin.Context) {
	if h.CloudInventory == nil || !h.CloudInventory.Enabled() {
		c.JSON(http.StatusOK, cloudCostsResponse{
			Enabled:   false,
			Providers: []model.CloudProvider{},
			Costs:     []cloudProviderCost{},
		})
		return
	}
	inv := h.CloudInventory.Inventory(c.Request.Context())
	c.JSON(http.StatusOK, projectCosts(inv))
}

// RefreshCloud busts the service cache and re-fetches synchronously,
// returning the fresh inventory. Backs POST /cloud/refresh (writeRole +
// rate limited). No provider configured -> 200 with enabled=false so the
// button is a harmless no-op on an unconfigured install.
func (h *Handler) RefreshCloud(c *gin.Context) {
	if h.CloudInventory == nil || !h.CloudInventory.Enabled() {
		c.JSON(http.StatusOK, emptyCloudInventory())
		return
	}
	c.JSON(http.StatusOK, h.CloudInventory.Refresh(c.Request.Context()))
}

// projectCosts pulls the cost summaries (and per-provider errors) out of
// an aggregated inventory snapshot into the costs response shape.
func projectCosts(inv model.CloudInventory) cloudCostsResponse {
	out := cloudCostsResponse{
		Enabled:   inv.Enabled,
		Providers: inv.Providers,
		FetchedAt: inv.FetchedAt,
		Costs:     make([]cloudProviderCost, 0, len(inv.Results)),
	}
	if out.Providers == nil {
		out.Providers = []model.CloudProvider{}
	}
	for _, r := range inv.Results {
		out.Costs = append(out.Costs, cloudProviderCost{
			Provider: r.Provider,
			Cost:     r.Cost,
			Error:    r.Error,
		})
	}
	return out
}
