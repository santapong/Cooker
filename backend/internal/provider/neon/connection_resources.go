package neon

import (
	"context"
	"strconv"
	"time"
)

func (s *ConnectionService) resourceConnection(ctx context.Context, id string, version int, projectID, branchID string) (*ConnectionRecord, error) {
	if !s.available() {
		return nil, connectionError("unavailable")
	}
	if !validConnectionID(id) || version < 1 || !validID(projectID) || (branchID != "" && !validBranchID(branchID)) {
		return nil, connectionError("invalid_input")
	}
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
	if r.Version != version {
		return nil, connectionError("conflict")
	}
	if !r.Enabled {
		return nil, connectionError("paused")
	}
	selected := false
	for _, v := range r.ProjectIDs {
		if v == projectID {
			selected = true
		}
	}
	if !selected {
		return nil, connectionError("scope_mismatch")
	}
	// The branch must belong to this selected project in a successful saved
	// inventory. A free-form route name never expands credential authority.
	if publicConnection(r, time.Now().UTC()).Snapshot == nil {
		return nil, connectionError("invalid_response")
	}
	if branchID != "" {
		found := false
		for _, b := range r.Snapshot.Branches {
			if b.ProjectID == projectID && b.ID == branchID {
				found = true
			}
		}
		if !found {
			return nil, connectionError("scope_mismatch")
		}
	}
	return r, nil
}

// ConnectionDatabases intentionally returns fresh, safe metadata, not a SQL
// health observation or a saved Vercel project/environment binding.
func (s *ConnectionService) ConnectionDatabases(ctx context.Context, id string, version int, projectID, branchID string) (*DatabaseInventory, error) {
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	r, err := s.resourceConnection(ctx, id, version, projectID, branchID)
	if err != nil {
		return nil, err
	}
	api, ok := s.API.(ResourceMetadataAPI)
	if !ok {
		return nil, connectionError("unavailable")
	}
	key, err := s.key(r)
	if err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	data, err := api.Databases(ctx, key, projectID, branchID)
	if err != nil {
		return nil, safeMetadataError(ctx, err)
	}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	if data == nil || !data.Complete || data.ProjectID != projectID || data.BranchID != branchID || data.Databases == nil || len(data.Databases) > maxItems || data.ObservedAt.Before(started) || data.ObservedAt.After(time.Now().UTC()) {
		return nil, connectionError("invalid_response")
	}
	seen := map[string]bool{}
	for _, d := range data.Databases {
		n, e := strconv.ParseInt(d.ID, 10, 64)
		if e != nil || n < 0 || strconv.FormatInt(n, 10) != d.ID || seen[d.ID] || d.ProjectID != projectID || d.BranchID != branchID {
			return nil, connectionError("scope_mismatch")
		}
		if _, e = safeName(d.Name); e != nil || d.Name == "" {
			return nil, connectionError("invalid_response")
		}
		seen[d.ID] = true
	}
	if _, err = s.resourceConnection(ctx, id, version, projectID, branchID); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	out := *data
	out.Databases = append([]Database{}, data.Databases...)
	return &out, nil
}

func (s *ConnectionService) ConnectionEndpoints(ctx context.Context, id string, version int, projectID string) (*EndpointInventory, error) {
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	r, err := s.resourceConnection(ctx, id, version, projectID, "")
	if err != nil {
		return nil, err
	}
	api, ok := s.API.(ResourceMetadataAPI)
	if !ok {
		return nil, connectionError("unavailable")
	}
	key, err := s.key(r)
	if err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	data, err := api.Endpoints(ctx, key, projectID)
	if err != nil {
		return nil, safeMetadataError(ctx, err)
	}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	if data == nil || !data.Complete || data.ProjectID != projectID || data.Endpoints == nil || len(data.Endpoints) > maxItems || data.ObservedAt.Before(started) || data.ObservedAt.After(time.Now().UTC()) {
		return nil, connectionError("invalid_response")
	}
	seen := map[string]bool{}
	for _, e := range data.Endpoints {
		if !validID(e.ID) || len(e.ID) < 4 || e.ID[:3] != "ep-" || seen[e.ID] || e.ProjectID != projectID || !validBranchID(e.BranchID) {
			return nil, connectionError("scope_mismatch")
		}
		if e.Type != "unknown" && e.Type != "read_only" && e.Type != "read_write" {
			return nil, connectionError("invalid_response")
		}
		if e.CurrentState != "unknown" && e.CurrentState != "init" && e.CurrentState != "active" && e.CurrentState != "idle" {
			return nil, connectionError("invalid_response")
		}
		if e.ComputeReleaseVersion != "" && !safeComputeRelease(e.ComputeReleaseVersion) {
			return nil, connectionError("invalid_response")
		}
		seen[e.ID] = true
	}
	if _, err = s.resourceConnection(ctx, id, version, projectID, ""); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	out := *data
	out.Endpoints = append([]Endpoint{}, data.Endpoints...)
	return &out, nil
}
