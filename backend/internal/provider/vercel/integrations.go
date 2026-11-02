package vercel

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"time"
)

const (
	integrationTimeout   = 15 * time.Second
	integrationBodyLimit = 1 << 20
	integrationLimit     = 200
)

// Only installation metadata is decoded. In particular scopes, environment
// values, resource metadata, billing, and arbitrary URLs are never retained.
type integrationWire struct {
	ID               string          `json:"id"`
	IntegrationID    string          `json:"integrationId"`
	Slug             string          `json:"slug"`
	InstallationType json.RawMessage `json:"installationType"`
	Status           json.RawMessage `json:"status"`
	Projects         json.RawMessage `json:"projects"`
	TeamID           json.RawMessage `json:"teamId"`
	OwnerID          json.RawMessage `json:"ownerId"`
	Integration      *struct {
		Name string `json:"name"`
	} `json:"integration"`
}

// Integrations makes one read of the documented top-level array, with the exact
// stored team scope. No pagination, retries, provider resources or Connect reads
// are inferred from this endpoint.
func (c *Client) Integrations(ctx context.Context, token, teamID string) ([]IntegrationInstallation, error) {
	ctx, cancel := context.WithTimeout(ctx, integrationTimeout)
	defer cancel()
	if err := validateScope(token, teamID); err != nil {
		return nil, err
	}
	query := scopeQuery(teamID)
	query.Set("view", "account")
	var body json.RawMessage
	if err := c.getWithBodyLimit(ctx, token, "/v1/integrations/configurations", query, &body, integrationBodyLimit); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, invalidResponse()
	}
	var rows []integrationWire
	if json.Unmarshal(trimmed, &rows) != nil {
		return nil, invalidResponse()
	}
	if len(rows) > integrationLimit {
		return nil, limitError("vercel integration inventory exceeds the supported limit")
	}
	items := make([]IntegrationInstallation, 0, len(rows))
	seen := map[string]bool{}
	for _, raw := range rows {
		if !validProviderID(raw.ID) || !validProviderID(raw.IntegrationID) || seen[raw.ID] {
			return nil, invalidResponse()
		}
		seen[raw.ID] = true
		if len(raw.TeamID) > 0 {
			if bytes.Equal(bytes.TrimSpace(raw.TeamID), []byte("null")) {
				if teamID != "" {
					return nil, invalidResponse()
				}
			} else {
				var ownerTeam string
				if json.Unmarshal(raw.TeamID, &ownerTeam) != nil || !validProviderID(ownerTeam) || ownerTeam != teamID {
					return nil, invalidResponse()
				}
			}
		}
		ownerID := ""
		if len(raw.OwnerID) > 0 && (json.Unmarshal(raw.OwnerID, &ownerID) != nil || !validProviderID(ownerID) || (teamID != "" && ownerID != teamID)) {
			return nil, invalidResponse()
		}
		kind, status := "external", "unknown"
		if len(raw.InstallationType) > 0 {
			value, ok := integrationString(raw.InstallationType)
			if !ok {
				return nil, invalidResponse()
			}
			kind = "unknown"
			if value == "external" || value == "marketplace" {
				kind = value
			}
		}
		if len(raw.Status) > 0 {
			value, ok := integrationString(raw.Status)
			if !ok {
				return nil, invalidResponse()
			}
			status = integrationStatus(value)
		}
		access, projects := "all_projects", []string{}
		if len(raw.Projects) > 0 {
			projectsJSON := bytes.TrimSpace(raw.Projects)
			if len(projectsJSON) == 0 || projectsJSON[0] != '[' || json.Unmarshal(projectsJSON, &projects) != nil {
				return nil, invalidResponse()
			}
			access = "selected_projects"
			var valid bool
			projects, valid = normalizedIntegrationProjects(projects)
			if !valid {
				return nil, invalidResponse()
			}
		}
		slug := displayText(raw.Slug, 128)
		name := ""
		if raw.Integration != nil {
			name = displayText(raw.Integration.Name, 256)
		}
		if name == "" {
			name = slug
		}
		if name == "" {
			name = raw.IntegrationID
		}
		items = append(items, IntegrationInstallation{ID: raw.ID, IntegrationID: raw.IntegrationID, Slug: slug, Name: name, InstallationType: kind, ProviderStatus: status, ProjectAccess: access, ProjectIDs: projects, ownerID: ownerID})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func integrationString(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", false
	}
	var value string
	if json.Unmarshal(trimmed, &value) != nil {
		return "", false
	}
	return value, true
}

func integrationStatus(value string) string {
	switch value {
	case "error", "onboarding", "pending", "ready", "resumed", "suspended", "uninstalled":
		return value
	default:
		return "unknown"
	}
}

func normalizedIntegrationProjects(projects []string) ([]string, bool) {
	out := make([]string, 0, len(projects))
	seen := map[string]bool{}
	for _, id := range projects {
		if !validProviderID(id) {
			return nil, false
		}
		if !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	sort.Strings(out)
	return out, true
}

func cloneIntegrationInventory(inventory *IntegrationInventory) *IntegrationInventory {
	if inventory == nil {
		return nil
	}
	out := *inventory
	out.Items = make([]IntegrationInstallation, len(inventory.Items))
	for i, item := range inventory.Items {
		out.Items[i] = item
		out.Items[i].ProjectIDs = append([]string{}, item.ProjectIDs...)
		out.Items[i].ownerID = ""
	}
	if inventory.ObservedAt != nil {
		t := *inventory.ObservedAt
		out.ObservedAt = &t
	}
	if inventory.AttemptedAt != nil {
		t := *inventory.AttemptedAt
		out.AttemptedAt = &t
	}
	return &out
}

// filterIntegrationInventory keeps both access forms limited to selected IDs.
func filterIntegrationInventory(inventory *IntegrationInventory, selected map[string]bool) *IntegrationInventory {
	out := cloneIntegrationInventory(inventory)
	if out == nil {
		return nil
	}
	out.Items = []IntegrationInstallation{}
	for _, item := range inventory.Items {
		ids := []string{}
		switch item.ProjectAccess {
		case "all_projects":
			for id, allowed := range selected {
				if allowed {
					ids = append(ids, id)
				}
			}
		case "selected_projects":
			for _, id := range item.ProjectIDs {
				if selected[id] {
					ids = append(ids, id)
				}
			}
		}
		if len(ids) == 0 {
			continue
		}
		ids, valid := normalizedIntegrationProjects(ids)
		if !valid {
			continue
		}
		item.ProjectIDs = ids
		item.ownerID = ""
		out.Items = append(out.Items, item)
	}
	return out
}

// Only a complete, successfully observed inventory can expose installation rows.
// Partial or legacy JSON may still carry rows without that success evidence.
func publicIntegrationInventory(inventory *IntegrationInventory, selected map[string]bool) *IntegrationInventory {
	out := filterIntegrationInventory(inventory, selected)
	if out != nil && (!out.Complete || out.ObservedAt == nil || out.ObservedAt.IsZero()) {
		out.Items = []IntegrationInstallation{}
		out.ObservedAt = nil
		out.Complete = false
		out.State = "unavailable"
	}
	return out
}
