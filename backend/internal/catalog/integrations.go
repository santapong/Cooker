package catalog

import (
	"sort"
	"time"

	"github.com/santapong/cooker/internal/provider/vercel"
)

type integrationChoice struct {
	connectionID string
	inventory    *vercel.IntegrationInventory
}

func cloneIntegrationTime(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	cp := *at
	return &cp
}

func cloneIntegrationInventory(inventory *vercel.IntegrationInventory) *vercel.IntegrationInventory {
	if inventory == nil {
		return nil
	}
	cp := *inventory
	cp.ObservedAt = cloneIntegrationTime(inventory.ObservedAt)
	cp.AttemptedAt = cloneIntegrationTime(inventory.AttemptedAt)
	cp.Items = make([]vercel.IntegrationInstallation, len(inventory.Items))
	for i, item := range inventory.Items {
		cp.Items[i] = item
		cp.Items[i].ProjectIDs = append([]string{}, item.ProjectIDs...)
	}
	return &cp
}

func observedIntegrations(inventory *vercel.IntegrationInventory) bool {
	return inventory != nil && inventory.Complete && inventory.ObservedAt != nil && !inventory.ObservedAt.IsZero()
}

// Installation access is filtered to the exact selected provider project. It
// does not establish a resource, a deployment dependency, or service health.
func projectIntegrationInventory(c *vercel.Connection, projectID string) *vercel.IntegrationInventory {
	if c == nil || c.Integrations == nil || c.Identity.ID == "" || projectID == "" {
		return nil
	}
	switch c.Identity.Scope {
	case "team":
		if c.TeamID != "" && c.TeamID != c.Identity.ID {
			return nil
		}
	case "personal":
		if c.TeamID != "" {
			return nil
		}
	default:
		return nil
	}
	selected := false
	for _, id := range c.ProjectIDs {
		if id == projectID {
			selected = true
			break
		}
	}
	if !selected {
		return nil
	}
	out := cloneIntegrationInventory(c.Integrations)
	out.Items = []vercel.IntegrationInstallation{}
	if !observedIntegrations(out) {
		// A failed/incomplete first attempt cannot provide an inventory, even if
		// an older writer supplied rows without a complete observation.
		out.ObservedAt = nil
		out.Complete = false
		out.State = "unavailable"
		return out
	}
	for _, item := range c.Integrations.Items {
		allowed := item.ProjectAccess == "all_projects"
		if item.ProjectAccess == "selected_projects" {
			for _, id := range item.ProjectIDs {
				if id == projectID {
					allowed = true
					break
				}
			}
		}
		if allowed {
			item.ProjectIDs = []string{projectID}
			out.Items = append(out.Items, item)
		}
	}
	sort.Slice(out.Items, func(i, j int) bool {
		if out.Items[i].ID == out.Items[j].ID {
			return out.Items[i].IntegrationID < out.Items[j].IntegrationID
		}
		return out.Items[i].ID < out.Items[j].ID
	})
	return out
}

// Merge whole observations within the caller's account/project key. The
// successful observation clock is independent of core and optional details.
// Retained rows and their attempt/error metadata always stay paired: a failed
// read with another token cannot relabel a successful observation as denied.
func preferIntegrationInventory(candidate, current integrationChoice) bool {
	if candidate.inventory == nil {
		return false
	}
	if current.inventory == nil {
		return true
	}
	candidateObserved := observedIntegrations(candidate.inventory)
	currentObserved := observedIntegrations(current.inventory)
	if candidateObserved != currentObserved {
		return candidateObserved
	}
	candidateAt, currentAt := candidate.inventory.AttemptedAt, current.inventory.AttemptedAt
	if candidateObserved {
		candidateAt, currentAt = candidate.inventory.ObservedAt, current.inventory.ObservedAt
	}
	if candidateAt != nil && (currentAt == nil || candidateAt.After(*currentAt)) {
		return true
	}
	if currentAt != nil && (candidateAt == nil || currentAt.After(*candidateAt)) {
		return false
	}
	if candidateObserved {
		candidateAvailable := candidate.inventory.State == "available" && candidate.inventory.Error == "" && candidate.inventory.ErrorCode == ""
		currentAvailable := current.inventory.State == "available" && current.inventory.Error == "" && current.inventory.ErrorCode == ""
		if candidateAvailable != currentAvailable {
			return candidateAvailable
		}
	}
	return candidate.connectionID < current.connectionID
}
