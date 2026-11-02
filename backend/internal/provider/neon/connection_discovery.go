package neon

import (
	"context"
	"time"
)

// DiscoverProjects inspects accessible projects in the saved organization,
// including before selection. It does not select projects, save a snapshot,
// advance sync clocks, infer SQL health or activate provider write operations.
func (s *ConnectionService) DiscoverProjects(ctx context.Context, id string, version int) (*ProjectInventory, error) {
	if !s.available() {
		return nil, connectionError("unavailable")
	}
	if !validConnectionID(id) || version < 1 {
		return nil, connectionError("invalid_input")
	}
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	r, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	if r.ID != id || r.Version != version {
		return nil, connectionError("conflict")
	}
	if !r.Enabled {
		return nil, connectionError("paused")
	}
	key, err := s.key(r)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	started := time.Now().UTC()
	inventory, err := s.API.Projects(ctx, key, r.OrganizationID)
	if err != nil {
		return nil, safeMetadataError(ctx, err)
	}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	if _, err = validateProjectInventory(inventory, r.OrganizationID, []string{}, started); err != nil {
		return nil, err
	}
	current, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	if current.ID != id || current.Version != version || current.OrganizationID != r.OrganizationID || !current.Enabled {
		return nil, connectionError("conflict")
	}
	out := *inventory
	out.Projects = append([]Project{}, inventory.Projects...)
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	return &out, nil
}
