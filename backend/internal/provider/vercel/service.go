package vercel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/crypto"
)

type Service struct {
	Store Store
	Codec *crypto.Codec
	API   API
}
type CreateRequest struct {
	Name   string `json:"name"`
	Token  string `json:"token"`
	TeamID string `json:"teamId"`
}
type UpdateRequest struct {
	Version    int       `json:"version"`
	Name       *string   `json:"name"`
	Token      *string   `json:"token"`
	Enabled    *bool     `json:"enabled"`
	ProjectIDs *[]string `json:"projectIds"`
}

func (s *Service) available() bool {
	return s != nil && s.Store != nil && s.API != nil && s.Codec.Active()
}
func validName(name string) bool {
	return len(strings.TrimSpace(name)) > 0 && len(name) <= 100 && !strings.ContainsAny(name, "\r\n\x00")
}
func validToken(token string) bool {
	return len(token) >= 10 && len(token) <= 4096 && !strings.ContainsAny(token, " \t\r\n\x00")
}
func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !valid {
			return false
		}
	}
	return true
}
func (s *Service) Create(ctx context.Context, req CreateRequest) (*Connection, error) {
	if !s.available() {
		return nil, ErrUnavailable
	}
	req.Name = strings.TrimSpace(req.Name)
	if !validName(req.Name) || !validToken(req.Token) || (req.TeamID != "" && !validID(req.TeamID)) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	identity, err := s.API.Verify(ctx, req.Token, req.TeamID)
	if err != nil {
		return nil, err
	}
	expectedScope := "personal"
	if req.TeamID != "" {
		expectedScope = "team"
	}
	if identity.ID == "" || identity.Scope != expectedScope || (req.TeamID != "" && identity.ID != req.TeamID) {
		return nil, ErrInvalid
	}
	sealed, err := s.Codec.Seal([]byte(req.Token))
	if err != nil {
		return nil, ErrUnavailable
	}
	now := time.Now().UTC()
	r := &Record{Connection: Connection{ID: uuid.NewString(), Name: req.Name, TeamID: req.TeamID, Identity: identity, Enabled: true, HasToken: true, Version: 1, ProjectIDs: []string{}, Credential: sealed, CreatedAt: now, UpdatedAt: now}, Snapshot: Snapshot{Projects: []Project{}, Deployments: []Deployment{}}}
	if err = s.Store.Create(ctx, r); err != nil {
		return nil, err
	}
	return publicConnection(r, now), nil
}
func (s *Service) token(r *Record) (string, error) {
	if !s.available() {
		return "", ErrUnavailable
	}
	b, err := s.Codec.Open(r.Credential)
	if err != nil {
		return "", ErrUnavailable
	}
	return string(b), nil
}
func (s *Service) Projects(ctx context.Context, id string) ([]Project, error) {
	if !s.available() {
		return nil, ErrUnavailable
	}
	r, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	token, err := s.token(r)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	if err = s.verifyIdentity(ctx, r, token); err != nil {
		return nil, err
	}
	projects, err := s.API.Projects(ctx, token, r.TeamID)
	if err != nil {
		return nil, err
	}
	latest, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if latest.Version != r.Version {
		return nil, ErrConflict
	}
	return projects, nil
}
func (s *Service) verifyIdentity(ctx context.Context, r *Record, token string) error {
	identity, err := s.API.Verify(ctx, token, r.TeamID)
	if err != nil {
		return err
	}
	if identity.ID != r.Identity.ID || identity.Scope != r.Identity.Scope {
		return fmt.Errorf("%w: token belongs to a different account or team", ErrInvalid)
	}
	return nil
}
func (s *Service) Update(ctx context.Context, id string, req UpdateRequest) (*Connection, error) {
	if !s.available() {
		return nil, ErrUnavailable
	}
	r, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Version != r.Version {
		return nil, ErrConflict
	}
	if req.Name != nil {
		r.Name = strings.TrimSpace(*req.Name)
		if !validName(r.Name) {
			return nil, ErrInvalid
		}
	}
	ctx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	var token string
	if req.Token == nil && req.ProjectIDs != nil {
		token, err = s.token(r)
		if err != nil {
			return nil, err
		}
	}
	if req.Token != nil {
		if !validToken(*req.Token) {
			return nil, ErrInvalid
		}
		token = *req.Token
		if err = s.verifyIdentity(ctx, r, token); err != nil {
			return nil, err
		}
		r.Credential, err = s.Codec.Seal([]byte(token))
		if err != nil {
			return nil, ErrUnavailable
		}
	}
	if req.ProjectIDs != nil {
		if len(*req.ProjectIDs) > MaxSelectedProjects {
			return nil, fmt.Errorf("%w: select at most %d projects", ErrInvalid, MaxSelectedProjects)
		}
		if err = s.verifyIdentity(ctx, r, token); err != nil {
			return nil, err
		}
		available, err := s.API.Projects(ctx, token, r.TeamID)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{}
		for _, p := range available {
			allowed[p.ID] = true
		}
		selected := []string{}
		seen := map[string]bool{}
		for _, p := range *req.ProjectIDs {
			if !validID(p) || !allowed[p] || seen[p] {
				return nil, fmt.Errorf("%w: project is duplicated or outside the connected scope", ErrInvalid)
			}
			selected = append(selected, p)
			seen[p] = true
		}
		sort.Strings(selected)
		r.ProjectIDs = selected
		r.Snapshot = filterSnapshot(r.Snapshot, seen)
	}
	if req.Enabled != nil {
		r.Enabled = *req.Enabled
	}
	// A configuration change invalidates any in-flight snapshot. Token rotation
	// retains provider backoff; only a successful sync clears retry state.
	r.UpdatedAt = time.Now().UTC()
	r.Lease = ""
	r.LeaseUntil = nil
	r.Syncing = false
	r.NextSyncAt = &r.UpdatedAt
	if err = s.Store.Update(ctx, r, req.Version); err != nil {
		return nil, err
	}
	latest, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return publicConnection(latest, time.Now().UTC()), nil
}
func (s *Service) Delete(ctx context.Context, id string, version int) error {
	if !s.available() {
		return ErrUnavailable
	}
	if version < 1 {
		return ErrInvalid
	}
	return s.Store.Delete(ctx, id, version)
}
func publicConnection(r *Record, now time.Time) *Connection {
	c := r.Connection
	c.Credential = nil
	c.Lease = ""
	c.Syncing = r.LeaseUntil != nil && r.LeaseUntil.After(now)
	c.LeaseUntil = nil
	c.ProjectIDs = append([]string{}, r.ProjectIDs...)
	c.Coverage = cloneCoverage(r.Snapshot.Coverage)
	selected := map[string]bool{}
	for _, id := range r.ProjectIDs {
		selected[id] = true
	}
	c.Integrations = publicIntegrationInventory(r.Snapshot.Integrations, selected)
	return &c
}
func filterSnapshot(snapshot Snapshot, selected map[string]bool) Snapshot {
	out := Snapshot{Projects: []Project{}, Deployments: []Deployment{}, Coverage: cloneCoverage(snapshot.Coverage), Integrations: filterIntegrationInventory(snapshot.Integrations, selected)}
	for _, p := range snapshot.Projects {
		if selected[p.ID] {
			out.Projects = append(out.Projects, p)
		}
	}
	for _, d := range snapshot.Deployments {
		if selected[d.ProjectID] {
			out.Deployments = append(out.Deployments, d)
		}
	}
	return out
}
func (s *Service) View(ctx context.Context) (*View, error) {
	if !s.available() {
		return nil, ErrUnavailable
	}
	records, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := &View{Connections: []*Connection{}, Projects: []ImportedProject{}, Deployments: []ImportedDeployment{}, SyncIntervalSeconds: int(SyncInterval / time.Second), HistoryLimit: HistoryLimit}
	now := time.Now().UTC()
	for _, r := range records {
		out.Connections = append(out.Connections, publicConnection(r, now))
		selected := map[string]bool{}
		for _, p := range r.ProjectIDs {
			selected[p] = true
		}
		snap := filterSnapshot(r.Snapshot, selected)
		for _, p := range snap.Projects {
			out.Projects = append(out.Projects, ImportedProject{Project: p, ConnectionID: r.ID})
		}
		for _, d := range snap.Deployments {
			out.Deployments = append(out.Deployments, ImportedDeployment{Deployment: d, ConnectionID: r.ID})
		}
	}
	sort.Slice(out.Deployments, func(i, j int) bool { return out.Deployments[i].CreatedAt.After(out.Deployments[j].CreatedAt) })
	return out, nil
}
func (s *Service) Sync(ctx context.Context, id string, manual bool) (*Connection, error) {
	if !s.available() {
		return nil, ErrUnavailable
	}
	r, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !r.Enabled || len(r.ProjectIDs) == 0 {
		return nil, ErrDisabled
	}
	now := time.Now().UTC()
	lease, err := s.Store.Claim(ctx, id, r.Version, now, now.Add(leaseDuration), manual)
	if err != nil {
		return nil, err
	}
	syncCtx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	snapshot, syncErr := s.fetch(syncCtx, r)
	next := time.Now().UTC().Add(SyncInterval)
	var retry *time.Time
	message := ""
	if syncErr == nil {
		if delay := s.enrich(syncCtx, r, snapshot); delay > 0 {
			next = time.Now().UTC().Add(delay)
			retry = &next
			message = "Vercel inventory refreshed; additional details are rate limited until retry"
		}
	}
	if syncErr != nil {
		message = safeSyncError(syncErr)
		delay := SyncInterval
		for i := 0; i < r.Failures && i < 4; i++ {
			delay *= 2
		}
		if delay > 30*time.Minute {
			delay = 30 * time.Minute
		}
		var apiErr *APIError
		if errors.As(syncErr, &apiErr) && apiErr.RetryAfter > delay {
			delay = apiErr.RetryAfter
		}
		if delay > 24*time.Hour {
			delay = 24 * time.Hour
		}
		next = time.Now().UTC().Add(delay)
		retry = &next
		snapshot = nil
	}
	// A canceled caller must still release its lease and record an honest failure.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	if err = s.Store.Finish(finishCtx, id, r.Version, lease, snapshot, message, next, retry); err != nil {
		return nil, err
	}
	latest, err := s.Store.Get(finishCtx, id)
	if err != nil {
		return nil, err
	}
	// Provider failure is recorded on the returned connection for both manual
	// refresh and background polling; the last good snapshot remains intact.
	return publicConnection(latest, time.Now().UTC()), nil
}
func safeSyncError(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Error()
	}
	if errors.Is(err, ErrInvalid) {
		return "connected account or project scope changed; review this connection"
	}
	if errors.Is(err, ErrUnavailable) {
		return "connection credential could not be decrypted; check server encryption configuration"
	}
	return "Vercel refresh failed; the last successful snapshot is retained"
}
func (s *Service) fetch(ctx context.Context, r *Record) (*Snapshot, error) {
	token, err := s.token(r)
	if err != nil {
		return nil, err
	}
	if err = s.verifyIdentity(ctx, r, token); err != nil {
		return nil, err
	}
	projects, err := s.API.Projects(ctx, token, r.TeamID)
	if err != nil {
		return nil, err
	}
	byID := map[string]Project{}
	for _, p := range projects {
		byID[p.ID] = p
	}
	out := &Snapshot{Projects: []Project{}, Deployments: []Deployment{}, Coverage: &ProjectCoverage{
		AccessibleProjects: len(byID), SelectedProjects: len(r.ProjectIDs), ObservedAt: time.Now().UTC(),
	}}
	for _, id := range r.ProjectIDs {
		p, ok := byID[id]
		if !ok {
			return nil, ErrInvalid
		}
		out.Projects = append(out.Projects, p)
		deployments, err := s.API.Deployments(ctx, token, r.TeamID, id)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, d := range deployments {
			if d.ID == "" || d.ProjectID != id {
				return nil, ErrInvalid
			}
			if seen[d.ID] {
				continue
			}
			seen[d.ID] = true
			out.Deployments = append(out.Deployments, d)
			if len(seen) >= HistoryLimit {
				break
			}
		}
	}
	return out, nil
}
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		if records, err := s.Store.List(ctx); err == nil {
			for _, r := range records {
				if ctx.Err() != nil {
					return
				}
				if !r.Enabled || len(r.ProjectIDs) == 0 {
					continue
				}
				_, _ = s.Sync(ctx, r.ID, false)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
