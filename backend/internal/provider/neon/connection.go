package neon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

type CreateConnectionRequest struct {
	Name                      string   `json:"name"`
	OrganizationID            string   `json:"organizationId"`
	AccountMode               string   `json:"accountMode"`
	APIKey                    string   `json:"apiKey"`
	ProjectIDs                []string `json:"projectIds"`
	AcknowledgeKeyPermissions bool     `json:"acknowledgeKeyPermissions"`
}

type UpdateConnectionRequest struct {
	Version                   int       `json:"version"`
	Name                      *string   `json:"name,omitempty"`
	AccountMode               *string   `json:"accountMode,omitempty"`
	APIKey                    *string   `json:"apiKey,omitempty"`
	Enabled                   *bool     `json:"enabled,omitempty"`
	ProjectIDs                *[]string `json:"projectIds,omitempty"`
	AcknowledgeKeyPermissions bool      `json:"acknowledgeKeyPermissions"`
}

type ConnectionService struct {
	Store ConnectionStore
	Codec CredentialCodec
	API   MetadataAPI
}

func (s *ConnectionService) available() bool {
	return s != nil && s.Store != nil && s.Codec != nil && s.Codec.Active() && s.API != nil
}

func newConnectionID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", connectionError("unavailable")
	}
	return prefix + hex.EncodeToString(value[:]), nil
}

func validConnectionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func validConnectionName(name string) bool {
	_, err := safeName(name)
	return err == nil && len(name) <= 100 && strings.TrimSpace(name) == name
}

func validAccountMode(value string) bool {
	return value == "unknown" || value == "neon_managed" || value == "vercel_managed"
}

func (s *ConnectionService) key(r *ConnectionRecord) (string, error) {
	if !s.available() {
		return "", connectionError("unavailable")
	}
	plain, err := s.Codec.Open(r.Credential)
	defer clear(plain)
	if err != nil || len(plain) > 8192 {
		return "", connectionError("unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.DisallowUnknownFields()
	var envelope connectionCredentialEnvelope
	if decoder.Decode(&envelope) != nil {
		return "", connectionError("unavailable")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || envelope.Purpose != ConnectionCredentialPurpose || envelope.ConnectionID != r.ID || envelope.OrganizationID != r.OrganizationID || !validKey(envelope.Key) {
		return "", connectionError("unavailable")
	}
	return envelope.Key, nil
}

func (s *ConnectionService) seal(id, org, key string) ([]byte, error) {
	plain, err := connectionEnvelopeBytes(id, org, key)
	defer clear(plain)
	if err != nil {
		return nil, connectionError("unavailable")
	}
	sealed, err := s.Codec.Seal(plain)
	if err != nil || len(sealed) == 0 || len(sealed) > 16384 {
		return nil, connectionError("unavailable")
	}
	return append([]byte{}, sealed...), nil
}

func safeMetadataError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	var provider *Error
	if errors.As(err, &provider) {
		switch provider.Code {
		case "unauthorized":
			return connectionError("provider_unauthorized")
		case "forbidden":
			return connectionError("provider_denied")
		case "rate_limited":
			return connectionError("rate_limited")
		case "scope_mismatch":
			return connectionError("scope_mismatch")
		case "canceled":
			return connectionError("canceled")
		case "invalid_response", "response_limit", "inventory_limit", "incomplete_coverage":
			return connectionError("invalid_response")
		}
	}
	return connectionError("provider_unavailable")
}

func validateProjectInventory(in *ProjectInventory, org string, selected []string, started time.Time) (map[string]Project, error) {
	if in == nil || !in.Complete || in.OrganizationID != org || in.ObservedAt.IsZero() || in.ObservedAt.Before(started) || in.ObservedAt.After(time.Now().UTC()) || in.Projects == nil || len(in.Projects) > maxItems {
		return nil, connectionError("invalid_response")
	}
	projects := map[string]Project{}
	for _, project := range in.Projects {
		if !validID(project.ID) || project.OrganizationID != org || !validID(project.RegionID) || project.CreatedAt.IsZero() || project.UpdatedAt.IsZero() {
			return nil, connectionError("scope_mismatch")
		}
		if _, err := safeName(project.Name); err != nil {
			return nil, connectionError("invalid_response")
		}
		if _, exists := projects[project.ID]; exists {
			return nil, connectionError("invalid_response")
		}
		projects[project.ID] = project
	}
	for _, id := range selected {
		if _, ok := projects[id]; !ok {
			return nil, connectionError("scope_mismatch")
		}
	}
	return projects, nil
}

func validateConnectionSnapshot(snapshot *ConnectionSnapshot, org string, selection []string) error {
	if snapshot == nil || !snapshot.Complete || snapshot.OrganizationID != org || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(time.Now().UTC()) || snapshot.Projects == nil || snapshot.Branches == nil || len(snapshot.Projects) != len(selection) || len(snapshot.Branches) > maxItems*MaxSelectedConnectionProjects {
		return connectionError("invalid_response")
	}
	selected := map[string]bool{}
	for _, id := range selection {
		selected[id] = true
	}
	seen := map[string]bool{}
	for _, p := range snapshot.Projects {
		if !selected[p.ID] || seen[p.ID] || p.OrganizationID != org || !validID(p.ID) || !validID(p.RegionID) || p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
			return connectionError("invalid_response")
		}
		if _, err := safeName(p.Name); err != nil {
			return connectionError("invalid_response")
		}
		seen[p.ID] = true
	}
	branches := map[string]bool{}
	for _, b := range snapshot.Branches {
		key := b.ProjectID + "/" + b.ID
		if !selected[b.ProjectID] || !validBranchID(b.ID) || branches[key] || (b.ParentID != "" && (!validBranchID(b.ParentID) || b.ParentID == b.ID)) || b.CreatedAt.IsZero() || b.UpdatedAt.IsZero() {
			return connectionError("invalid_response")
		}
		if _, err := safeName(b.Name); err != nil {
			return connectionError("invalid_response")
		}
		branches[key] = true
	}
	return nil
}

func (s *ConnectionService) Create(ctx context.Context, req CreateConnectionRequest) (*Connection, error) {
	if !s.available() {
		return nil, connectionError("unavailable")
	}
	req.Name = strings.TrimSpace(req.Name)
	if !validConnectionName(req.Name) || !validID(req.OrganizationID) || !validAccountMode(req.AccountMode) || !validKey(req.APIKey) || !req.AcknowledgeKeyPermissions {
		return nil, connectionError("invalid_input")
	}
	selection, err := normalizedProjectSelection(req.ProjectIDs)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, ConnectionSyncTimeout)
	defer cancel()
	started := time.Now().UTC()
	inventory, err := s.API.Projects(ctx, req.APIKey, req.OrganizationID)
	if err != nil {
		return nil, safeMetadataError(ctx, err)
	}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	if _, err = validateProjectInventory(inventory, req.OrganizationID, selection, started); err != nil {
		return nil, err
	}
	id, err := newConnectionID("nconn_")
	if err != nil {
		return nil, err
	}
	sealed, err := s.seal(id, req.OrganizationID, req.APIKey)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	r := &ConnectionRecord{Connection: Connection{ID: id, Name: req.Name, OrganizationID: req.OrganizationID, AccountMode: req.AccountMode, Enabled: true, Version: 1, ProjectIDs: selection, CreatedAt: now, UpdatedAt: now}, Credential: sealed}
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	if err = s.Store.Create(ctx, r); err != nil {
		return nil, err
	}
	return publicConnection(r, now), nil
}

func (s *ConnectionService) Update(ctx context.Context, id string, req UpdateConnectionRequest) (*Connection, error) {
	if !s.available() {
		return nil, connectionError("unavailable")
	}
	if !validConnectionID(id) || req.Version < 1 {
		return nil, connectionError("invalid_input")
	}
	ctx, cancel := context.WithTimeout(ctx, ConnectionSyncTimeout)
	defer cancel()
	r, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.Version != req.Version {
		return nil, connectionError("conflict")
	}
	if req.Name != nil {
		r.Name = strings.TrimSpace(*req.Name)
		if !validConnectionName(r.Name) {
			return nil, connectionError("invalid_input")
		}
	}
	if req.AccountMode != nil {
		if !validAccountMode(*req.AccountMode) {
			return nil, connectionError("invalid_input")
		}
		r.AccountMode = *req.AccountMode
	}
	if req.Enabled != nil {
		r.Enabled = *req.Enabled
	}
	if req.ProjectIDs != nil {
		r.ProjectIDs, err = normalizedProjectSelection(*req.ProjectIDs)
		if err != nil {
			return nil, err
		}
	}
	if req.APIKey != nil || req.ProjectIDs != nil {
		if !r.Enabled {
			return nil, connectionError("paused")
		}
		var key string
		if req.APIKey != nil {
			if !req.AcknowledgeKeyPermissions || !validKey(*req.APIKey) {
				return nil, connectionError("invalid_input")
			}
			key = *req.APIKey
		} else {
			key, err = s.key(r)
			if err != nil {
				return nil, err
			}
		}
		started := time.Now().UTC()
		inventory, err := s.API.Projects(ctx, key, r.OrganizationID)
		if err != nil {
			return nil, safeMetadataError(ctx, err)
		}
		if ctx.Err() != nil {
			return nil, connectionError("canceled")
		}
		if _, err = validateProjectInventory(inventory, r.OrganizationID, r.ProjectIDs, started); err != nil {
			return nil, err
		}
		if req.APIKey != nil {
			r.Credential, err = s.seal(r.ID, r.OrganizationID, key)
			if err != nil {
				return nil, err
			}
		}
	}
	r.UpdatedAt = time.Now().UTC()
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	if err = s.Store.Update(ctx, r, req.Version); err != nil {
		return nil, err
	}
	latest, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return publicConnection(latest, time.Now().UTC()), nil
}

func (s *ConnectionService) Get(ctx context.Context, id string) (*Connection, error) {
	if s == nil || s.Store == nil {
		return nil, connectionError("unavailable")
	}
	if !validConnectionID(id) {
		return nil, connectionError("invalid_input")
	}
	r, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return publicConnection(r, time.Now().UTC()), nil
}

func (s *ConnectionService) List(ctx context.Context) ([]*Connection, error) {
	if s == nil || s.Store == nil {
		return nil, connectionError("unavailable")
	}
	rows, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxNeonConnections {
		return nil, connectionError("connection_limit")
	}
	out := make([]*Connection, 0, len(rows))
	for _, r := range rows {
		out = append(out, publicConnection(r, time.Now().UTC()))
	}
	return out, nil
}

func (s *ConnectionService) Delete(ctx context.Context, id string, version int) error {
	if s == nil || s.Store == nil {
		return connectionError("unavailable")
	}
	if !validConnectionID(id) || version < 1 {
		return connectionError("invalid_input")
	}
	return s.Store.Delete(ctx, id, version)
}

func (s *ConnectionService) refreshCurrent(ctx context.Context, original *ConnectionRecord, lease string) error {
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	current, err := s.Store.Get(ctx, original.ID)
	if err != nil {
		return err
	}
	if current.Version != original.Version || !current.Enabled || current.OrganizationID != original.OrganizationID || !sameProjectSelection(current.ProjectIDs, original.ProjectIDs) || current.Lease != lease || current.LeaseUntil == nil || !current.LeaseUntil.After(time.Now().UTC()) {
		return connectionError("conflict")
	}
	return nil
}

func (s *ConnectionService) Sync(ctx context.Context, id string) (*Connection, error) {
	if !s.available() {
		return nil, connectionError("unavailable")
	}
	if !validConnectionID(id) {
		return nil, connectionError("invalid_input")
	}
	ctx, cancel := context.WithTimeout(ctx, ConnectionSyncTimeout)
	defer cancel()
	r, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !r.Enabled || len(r.ProjectIDs) == 0 {
		return nil, connectionError("paused")
	}
	lease, err := newConnectionID("nlease_")
	if err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	if err = s.Store.Claim(ctx, id, r.Version, lease, started, started.Add(ConnectionLeaseDuration)); err != nil {
		return nil, err
	}
	var snapshot *ConnectionSnapshot
	key, readErr := s.key(r)
	if readErr == nil {
		readErr = s.refreshCurrent(ctx, r, lease)
	}
	if readErr == nil {
		var inventory *ProjectInventory
		inventory, readErr = s.API.Projects(ctx, key, r.OrganizationID)
		if readErr != nil {
			readErr = safeMetadataError(ctx, readErr)
		} else if ctx.Err() != nil {
			readErr = connectionError("canceled")
		} else {
			var available map[string]Project
			available, readErr = validateProjectInventory(inventory, r.OrganizationID, r.ProjectIDs, started)
			if readErr == nil {
				snapshot = &ConnectionSnapshot{OrganizationID: r.OrganizationID, Projects: []Project{}, Branches: []Branch{}}
				for _, projectID := range r.ProjectIDs {
					snapshot.Projects = append(snapshot.Projects, available[projectID])
					if readErr = s.refreshCurrent(ctx, r, lease); readErr != nil {
						break
					}
					branches, branchErr := s.API.Branches(ctx, key, projectID)
					if branchErr != nil {
						readErr = safeMetadataError(ctx, branchErr)
						break
					}
					if ctx.Err() != nil {
						readErr = connectionError("canceled")
						break
					}
					if branches == nil || !branches.Complete || branches.ProjectID != projectID || branches.Branches == nil || len(branches.Branches) > maxItems || branches.ObservedAt.Before(started) || branches.ObservedAt.After(time.Now().UTC()) {
						readErr = connectionError("invalid_response")
						break
					}
					for _, branch := range branches.Branches {
						if branch.ProjectID != projectID {
							readErr = connectionError("scope_mismatch")
							break
						}
					}
					if readErr != nil {
						break
					}
					snapshot.Branches = append(snapshot.Branches, branches.Branches...)
				}
				if readErr == nil {
					snapshot.Complete = true
					snapshot.ObservedAt = time.Now().UTC()
					readErr = validateConnectionSnapshot(snapshot, r.OrganizationID, r.ProjectIDs)
				}
			}
		}
	}
	if ctx.Err() != nil {
		readErr = connectionError("canceled")
	}
	code := ""
	if readErr != nil {
		snapshot = nil
		var safe *RequestError
		if errors.As(readErr, &safe) && safeConnectionCode(safe.Code) {
			code = safe.Code
		} else {
			code = "provider_unavailable"
		}
	}
	// A canceled HTTP request may still release its GET-only lease and record a
	// fixed failure. This bounded local write never advances a success clock.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer finishCancel()
	if err = s.Store.Finish(finishCtx, id, r.Version, lease, snapshot, code, time.Now().UTC()); err != nil {
		return nil, err
	}
	latest, err := s.Store.Get(finishCtx, id)
	if err != nil {
		return nil, err
	}
	return publicConnection(latest, time.Now().UTC()), nil
}
