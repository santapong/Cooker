package vercel

import (
	"context"
	"errors"
	"time"
)

func (s *Service) enrichIntegrations(ctx context.Context, r *Record, out *Snapshot, cooldown time.Duration) time.Duration {
	selected := map[string]bool{}
	for _, id := range r.ProjectIDs {
		selected[id] = true
	}
	retain := func(code, message string, attempted *time.Time) {
		inventory := filterIntegrationInventory(r.Snapshot.Integrations, selected)
		if inventory == nil {
			inventory = &IntegrationInventory{Items: []IntegrationInstallation{}}
		}
		if inventory.ObservedAt == nil || inventory.ObservedAt.IsZero() || !inventory.Complete {
			inventory.Items = []IntegrationInstallation{}
			inventory.ObservedAt = nil
			inventory.Complete = false
			inventory.State = "unavailable"
		} else {
			inventory.State = "stale"
		}
		if attempted != nil {
			t := *attempted
			inventory.AttemptedAt = &t
		}
		inventory.ErrorCode, inventory.Error = code, message
		out.Integrations = inventory
	}
	api, supported := s.API.(IntegrationsAPI)
	if !supported {
		retain("unsupported", "Integration inventory is unavailable with this connection", nil)
		return 0
	}
	if cooldown > 0 {
		retain("rate_limited", "Integration inventory was not refreshed because optional reads are rate limited", nil)
		return cooldown
	}
	if err := ctx.Err(); err != nil {
		code, message := integrationError(err)
		retain(code, message, nil)
		return 0
	}
	token, err := s.token(r)
	if err != nil {
		retain("credential_unavailable", "Integration inventory could not be refreshed with this connection", nil)
		return 0
	}
	attempted := time.Now().UTC()
	requestCtx, cancel := context.WithTimeout(ctx, integrationTimeout)
	defer cancel()
	items, err := api.Integrations(requestCtx, token, r.TeamID)
	if err == nil {
		err = requestCtx.Err()
	}
	if err == nil {
		err = validateIntegrationItems(items, r.Identity.ID)
	}
	if err != nil {
		code, message := integrationError(err)
		retain(code, message, &attempted)
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == 429 || apiErr.RetryAfter > 0) {
			delay := apiErr.RetryAfter
			if delay < SyncInterval {
				delay = SyncInterval
			}
			if delay > 24*time.Hour {
				delay = 24 * time.Hour
			}
			return delay
		}
		return 0
	}
	observed := time.Now().UTC()
	out.Integrations = filterIntegrationInventory(&IntegrationInventory{Items: items, ObservedAt: &observed, AttemptedAt: &attempted, State: "available", Complete: true}, selected)
	return 0
}

// Optional adapters cannot bypass the installation identity/access boundary or
// smuggle unbounded display text through the normalized interface.
func validateIntegrationItems(items []IntegrationInstallation, ownerID string) error {
	if len(items) > integrationLimit {
		return limitError("vercel integration inventory exceeds the supported limit")
	}
	seen := map[string]bool{}
	for _, item := range items {
		if !validProviderID(item.ID) || !validProviderID(item.IntegrationID) || seen[item.ID] || (item.ownerID != "" && item.ownerID != ownerID) {
			return invalidResponse()
		}
		seen[item.ID] = true
		if displayText(item.Name, 256) != item.Name || displayText(item.Slug, 128) != item.Slug {
			return invalidResponse()
		}
		if item.InstallationType != "marketplace" && item.InstallationType != "external" && item.InstallationType != "unknown" {
			return invalidResponse()
		}
		if integrationStatus(item.ProviderStatus) != item.ProviderStatus {
			return invalidResponse()
		}
		if item.ProjectAccess != "all_projects" && item.ProjectAccess != "selected_projects" {
			return invalidResponse()
		}
		if _, valid := normalizedIntegrationProjects(item.ProjectIDs); !valid {
			return invalidResponse()
		}
	}
	return nil
}

// Every persisted warning is Cooker-authored; APIError.Message and arbitrary
// adapter/transport errors never become installation inventory copy.
func integrationError(err error) (string, string) {
	if errors.Is(err, context.Canceled) {
		return "canceled", "Integration inventory refresh was canceled; any previous observation is retained"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", "Integration inventory refresh timed out; any previous observation is retained"
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 401 || apiErr.Code == "unauthorized":
			return "unauthorized", "Integration inventory is unavailable with this connection's authentication; deployment records remain available"
		case apiErr.StatusCode == 403 || apiErr.Code == "forbidden":
			return "forbidden", "Integration inventory is unavailable with this connection's permissions; deployment records remain available"
		case apiErr.StatusCode == 429 || apiErr.RetryAfter > 0 || apiErr.Code == "rate_limited":
			return "rate_limited", "Integration inventory is rate limited; any previous observation is retained until retry"
		case apiErr.Code == "invalid_response":
			return "invalid_response", "Integration inventory returned invalid metadata; any previous observation is retained"
		case apiErr.Code == "inventory_limit":
			return "inventory_limit", "Integration inventory exceeded the supported read limit; any previous observation is retained"
		case apiErr.Code == "timeout":
			return "timeout", "Integration inventory refresh timed out; any previous observation is retained"
		case apiErr.Code == "canceled":
			return "canceled", "Integration inventory refresh was canceled; any previous observation is retained"
		case apiErr.Code == "redirect_blocked":
			return "redirect_blocked", "Integration inventory redirect was refused; any previous observation is retained"
		case apiErr.StatusCode == 404 || apiErr.StatusCode == 410:
			return "unavailable", "Integration inventory is unavailable from Vercel; any previous observation is retained"
		}
	}
	return "unavailable", "Integration inventory could not be refreshed; any previous observation is retained"
}
