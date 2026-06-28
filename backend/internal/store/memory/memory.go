// Package memory provides an in-memory implementation of the store
// interfaces. Intended for unit tests and local development when a
// PostgreSQL instance is not available.
package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// New returns an in-memory aggregate store. Safe for concurrent use.
func New() *store.Store {
	return store.New(
		&pipelines{m: map[string]*model.Pipeline{}},
		&runs{m: map[string]*model.PipelineRun{}},
		&environments{m: map[string]*model.Environment{}},
		&promotions{m: map[string]*model.RunPromotion{}, approvals: map[string][]model.PromotionApproval{}},
		&stageApprovals{m: map[string]*model.StageApproval{}, votes: map[string][]model.StageApprovalVote{}},
		&apps{m: map[string]*model.App{}},
		&appDeploys{m: map[string]*model.AppDeploy{}},
		&auditEvents{},
		&hosts{m: map[string]*model.Host{}},
		&registryConfigs{m: map[string]*model.RegistryConfig{}},
		&clusterConfigs{m: map[string]*model.ClusterConfig{}},
		&users{byID: map[string]*model.User{}, byEmail: map[string]string{}},
		&apiTokens{m: map[string]*model.APIToken{}},
		&licenses{},
		nil,
		nil,
	)
}

type pipelines struct {
	mu sync.RWMutex
	m  map[string]*model.Pipeline
}

func (s *pipelines) List(_ context.Context) ([]*model.Pipeline, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Pipeline, 0, len(s.m))
	for _, p := range s.m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *pipelines) Get(_ context.Context, id string) (*model.Pipeline, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("pipeline %s: %w", id, store.ErrNotFound)
	}
	return p, nil
}

func (s *pipelines) Create(_ context.Context, p *model.Pipeline) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[p.ID] = p
	return nil
}

func (s *pipelines) Update(_ context.Context, p *model.Pipeline) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[p.ID]
	if !ok {
		return fmt.Errorf("pipeline %s: %w", p.ID, store.ErrNotFound)
	}
	if cur.Version != p.Version {
		return fmt.Errorf("pipeline %s: %w", p.ID, store.ErrConflict)
	}
	p.Version++
	s.m[p.ID] = p
	return nil
}

func (s *pipelines) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("pipeline %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}

type runs struct {
	mu sync.RWMutex
	m  map[string]*model.PipelineRun
}

func (s *runs) List(_ context.Context, pipelineID string, limit, offset int) ([]*model.PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	matched := make([]*model.PipelineRun, 0)
	for _, r := range s.m {
		if r.PipelineID == pipelineID {
			matched = append(matched, r)
		}
	}
	// Sort newest-first to match the Postgres ORDER BY created_at DESC.
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	if offset > 0 {
		if offset >= len(matched) {
			return []*model.PipelineRun{}, nil
		}
		matched = matched[offset:]
	}
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	// Return copies with per-stage Logs stripped, matching the Postgres
	// list projection. Stored rows must not be mutated — callers of Get
	// still see full logs.
	out := make([]*model.PipelineRun, len(matched))
	for i, r := range matched {
		cp := *r
		if len(r.StageRuns) > 0 {
			srs := make([]model.StageRun, len(r.StageRuns))
			copy(srs, r.StageRuns)
			for j := range srs {
				srs[j].Logs = ""
			}
			cp.StageRuns = srs
		}
		out[i] = &cp
	}
	return out, nil
}

func (s *runs) Get(_ context.Context, id string) (*model.PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("run %s: %w", id, store.ErrNotFound)
	}
	// Return a shallow copy so the caller cannot race the heartbeat ticker
	// or the executor, both of which mutate the stored pointer concurrently
	// (DA-H1). The executor replaces StageRuns, EnvironmentStatuses, and
	// Variables slices wholesale, so a shallow copy of those slice headers
	// is sufficient — concurrent writes land on a different slice.
	cp := *r
	return &cp, nil
}

func (s *runs) Create(_ context.Context, r *model.PipelineRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	s.m[r.ID] = r
	return nil
}

func (s *runs) Update(_ context.Context, r *model.PipelineRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[r.ID]; !ok {
		return fmt.Errorf("run %s: %w", r.ID, store.ErrNotFound)
	}
	s.m[r.ID] = r
	return nil
}

func (s *runs) UpdateStatus(_ context.Context, id string, status model.RunStatus, finishedAt *time.Time, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[id]
	if !ok {
		return fmt.Errorf("run %s: %w", id, store.ErrNotFound)
	}
	// Mutate only the lifecycle fields in place (mirrors UpdateHeartbeat's
	// targeted style); leaves StageRuns / HeartbeatAt untouched.
	r.Status = status
	r.FinishedAt = finishedAt
	r.Error = errMsg
	return nil
}

func (s *runs) UpdateHeartbeat(_ context.Context, id string, ts time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[id]
	if !ok {
		return fmt.Errorf("run %s: %w", id, store.ErrNotFound)
	}
	t := ts
	r.HeartbeatAt = &t
	return nil
}

func (s *runs) SweepOrphans(_ context.Context, threshold time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	swept := 0
	for _, r := range s.m {
		if r.Status != model.RunStatusRunning {
			continue
		}
		if r.HeartbeatAt == nil || now.Sub(*r.HeartbeatAt) > threshold {
			r.Status = model.RunStatusFailed
			r.Error = "orphaned: heartbeat stale at boot"
			finished := now
			r.FinishedAt = &finished
			swept++
		}
	}
	return swept, nil
}

type environments struct {
	mu sync.RWMutex
	m  map[string]*model.Environment
}

func (s *environments) List(_ context.Context) ([]*model.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Environment, 0, len(s.m))
	for _, e := range s.m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
}

func (s *environments) Get(_ context.Context, id string) (*model.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("environment %s: %w", id, store.ErrNotFound)
	}
	return e, nil
}

func (s *environments) Create(_ context.Context, e *model.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[e.ID] = e
	return nil
}

func (s *environments) Update(_ context.Context, e *model.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[e.ID]
	if !ok {
		return fmt.Errorf("environment %s: %w", e.ID, store.ErrNotFound)
	}
	if cur.Version != e.Version {
		return fmt.Errorf("environment %s: %w", e.ID, store.ErrConflict)
	}
	e.Version++
	s.m[e.ID] = e
	return nil
}

func (s *environments) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("environment %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}

// promotions is the in-memory PromotionStore. Promotions are keyed by
// id; approvals live in a side map keyed by promotion id. Mirrors the
// Postgres impl, including the (run, environment) uniqueness on create
// and the one-approval-per-approver dedupe on AddApproval.
type promotions struct {
	mu        sync.RWMutex
	m         map[string]*model.RunPromotion
	approvals map[string][]model.PromotionApproval
}

// findByRunEnvLocked returns the promotion for (runID, environmentID).
// Caller must hold at least a read lock.
func (s *promotions) findByRunEnvLocked(runID, environmentID string) *model.RunPromotion {
	for _, p := range s.m {
		if p.RunID == runID && p.EnvironmentID == environmentID {
			return p
		}
	}
	return nil
}

// hydrate returns a deep-ish copy of p with its Approvals slice filled
// from the side map, so callers never see the stored pointer or a
// shared approvals slice.
func (s *promotions) hydrate(p *model.RunPromotion) *model.RunPromotion {
	cp := *p
	stored := s.approvals[p.ID]
	cp.Approvals = make([]model.PromotionApproval, len(stored))
	copy(cp.Approvals, stored)
	return &cp
}

func (s *promotions) CreatePromotion(_ context.Context, p *model.RunPromotion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findByRunEnvLocked(p.RunID, p.EnvironmentID) != nil {
		return fmt.Errorf("promotion %s/%s: %w", p.RunID, p.EnvironmentID, store.ErrConflict)
	}
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = p.CreatedAt
	cp := *p
	cp.Approvals = nil
	s.m[p.ID] = &cp
	return nil
}

func (s *promotions) GetPromotion(_ context.Context, runID, environmentID string) (*model.RunPromotion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.findByRunEnvLocked(runID, environmentID)
	if p == nil {
		return nil, fmt.Errorf("promotion %s/%s: %w", runID, environmentID, store.ErrNotFound)
	}
	return s.hydrate(p), nil
}

func (s *promotions) ListPromotions(_ context.Context, runID string) ([]*model.RunPromotion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.RunPromotion, 0)
	for _, p := range s.m {
		if p.RunID == runID {
			out = append(out, s.hydrate(p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *promotions) UpdatePromotionStatus(_ context.Context, id string, status model.PromotionStatus, promotedAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[id]
	if !ok {
		return fmt.Errorf("promotion %s: %w", id, store.ErrNotFound)
	}
	p.Status = status
	if promotedAt != nil {
		t := *promotedAt
		p.PromotedAt = &t
	}
	p.UpdatedAt = time.Now()
	return nil
}

func (s *promotions) AddApproval(_ context.Context, a *model.PromotionApproval) (bool, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[a.PromotionID]; !ok {
		return false, 0, fmt.Errorf("promotion %s: %w", a.PromotionID, store.ErrNotFound)
	}
	existing := s.approvals[a.PromotionID]
	for i := range existing {
		if existing[i].ApproverSub == a.ApproverSub {
			// Idempotent: same approver, no new row, count unchanged.
			return false, len(existing), nil
		}
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	s.approvals[a.PromotionID] = append(existing, *a)
	return true, len(existing) + 1, nil
}

type apps struct {
	mu sync.RWMutex
	m  map[string]*model.App
}

func (s *apps) List(_ context.Context) ([]*model.App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.App, 0, len(s.m))
	for _, a := range s.m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *apps) Get(_ context.Context, id string) (*model.App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	return a, nil
}

func (s *apps) GetByRepo(_ context.Context, repo, branch string) (*model.App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.m {
		if a.GitHubRepo == repo && a.Branch == branch {
			return a, nil
		}
	}
	return nil, fmt.Errorf("app %s@%s: %w", repo, branch, store.ErrNotFound)
}

func (s *apps) Create(_ context.Context, a *model.App) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[a.ID] = a
	return nil
}

func (s *apps) Update(_ context.Context, a *model.App) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[a.ID]
	if !ok {
		return fmt.Errorf("app %s: %w", a.ID, store.ErrNotFound)
	}
	if cur.Version != a.Version {
		return fmt.Errorf("app %s: %w", a.ID, store.ErrConflict)
	}
	a.Version++
	s.m[a.ID] = a
	return nil
}

func (s *apps) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}

// UpdateHealth writes the latest probe verdict in place. Does NOT
// bump Version — health is observational state, not user-visible
// configuration, so a probe write should never block a concurrent
// Update via optimistic-concurrency conflict.
// deployedURL may be empty; an empty string leaves the existing value intact.
func (s *apps) UpdateHealth(_ context.Context, id string, status model.AppHealth, msg string, at time.Time, deployedURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[id]
	if !ok {
		return fmt.Errorf("app %s: %w", id, store.ErrNotFound)
	}
	cur.HealthStatus = status
	cur.HealthMessage = msg
	tt := at
	cur.HealthCheckedAt = &tt
	if deployedURL != "" {
		cur.DeployedURL = deployedURL
	}
	return nil
}

type hosts struct {
	mu sync.RWMutex
	m  map[string]*model.Host
}

func (s *hosts) List(_ context.Context) ([]*model.Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Host, 0, len(s.m))
	for _, h := range s.m {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *hosts) Get(_ context.Context, id string) (*model.Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("host %s: %w", id, store.ErrNotFound)
	}
	return h, nil
}

func (s *hosts) Create(_ context.Context, h *model.Host) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[h.ID] = h
	return nil
}

func (s *hosts) Update(_ context.Context, h *model.Host) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[h.ID]
	if !ok {
		return fmt.Errorf("host %s: %w", h.ID, store.ErrNotFound)
	}
	if cur.Version != h.Version {
		return fmt.Errorf("host %s: %w", h.ID, store.ErrConflict)
	}
	h.Version++
	s.m[h.ID] = h
	return nil
}

func (s *hosts) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("host %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}

// registryConfigs is the in-memory RegistryConfigStore. Mirrors the
// hosts impl: name-ordered List, ErrNotFound on missing Get/Delete.
type registryConfigs struct {
	mu sync.RWMutex
	m  map[string]*model.RegistryConfig
}

func (s *registryConfigs) List(_ context.Context) ([]*model.RegistryConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.RegistryConfig, 0, len(s.m))
	for _, r := range s.m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *registryConfigs) Get(_ context.Context, id string) (*model.RegistryConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("registry config %s: %w", id, store.ErrNotFound)
	}
	return r, nil
}

func (s *registryConfigs) Create(_ context.Context, r *model.RegistryConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[r.ID] = r
	return nil
}

func (s *registryConfigs) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("registry config %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}

// clusterConfigs is the in-memory ClusterConfigStore. Same shape as
// registryConfigs.
type clusterConfigs struct {
	mu sync.RWMutex
	m  map[string]*model.ClusterConfig
}

func (s *clusterConfigs) List(_ context.Context) ([]*model.ClusterConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.ClusterConfig, 0, len(s.m))
	for _, c := range s.m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *clusterConfigs) Get(_ context.Context, id string) (*model.ClusterConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("cluster config %s: %w", id, store.ErrNotFound)
	}
	return c, nil
}

func (s *clusterConfigs) Create(_ context.Context, c *model.ClusterConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[c.ID] = c
	return nil
}

func (s *clusterConfigs) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("cluster config %s: %w", id, store.ErrNotFound)
	}
	delete(s.m, id)
	return nil
}

// users is an in-memory store.UserStore. Email keys are case-insensitive
// (lowercased on the way in) so callers don't have to normalise.
type users struct {
	mu      sync.RWMutex
	byID    map[string]*model.User
	byEmail map[string]string // lowercased email → id
}

func (s *users) GetByEmail(_ context.Context, email string) (*model.User, error) {
	key := strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byEmail[key]
	if !ok {
		return nil, fmt.Errorf("user %s: %w", email, store.ErrNotFound)
	}
	return s.byID[id], nil
}

func (s *users) GetByID(_ context.Context, id string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, fmt.Errorf("user %s: %w", id, store.ErrNotFound)
	}
	return u, nil
}

func (s *users) Create(_ context.Context, u *model.User) error {
	key := strings.ToLower(strings.TrimSpace(u.Email))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[key]; exists {
		return fmt.Errorf("user %s: already exists", u.Email)
	}
	s.byID[u.ID] = u
	s.byEmail[key] = u.ID
	return nil
}

func (s *users) Update(_ context.Context, u *model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.byID[u.ID]
	if !ok {
		return fmt.Errorf("user %s: %w", u.ID, store.ErrNotFound)
	}
	if !strings.EqualFold(old.Email, u.Email) {
		delete(s.byEmail, strings.ToLower(strings.TrimSpace(old.Email)))
		s.byEmail[strings.ToLower(strings.TrimSpace(u.Email))] = u.ID
	}
	s.byID[u.ID] = u
	return nil
}

func (s *users) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID), nil
}

// appDeploys is the in-memory deploy-history store. Append-only,
// newest-first reads, mirroring the Postgres impl (default limit 20).
type appDeploys struct {
	mu sync.RWMutex
	m  map[string]*model.AppDeploy
}

func (s *appDeploys) Create(_ context.Context, d *model.AppDeploy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	s.m[d.ID] = d
	return nil
}

func (s *appDeploys) ListByApp(_ context.Context, appID string, limit int) ([]*model.AppDeploy, error) {
	if limit <= 0 {
		limit = 20
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.AppDeploy, 0)
	for _, d := range s.m {
		if d.AppID == appID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *appDeploys) Get(_ context.Context, id string) (*model.AppDeploy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("app deploy %s: %w", id, store.ErrNotFound)
	}
	return d, nil
}

// auditEvents is the in-memory audit trail: a bounded ring (~10k)
// for dev parity with the Postgres impl. Older events fall off the
// front; nothing here survives a restart.
type auditEvents struct {
	mu     sync.RWMutex
	events []*model.AuditEvent
	nextID int64
}

const auditRingMax = 10_000

func (s *auditEvents) Insert(_ context.Context, e *model.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	e.ID = s.nextID
	s.events = append(s.events, e)
	if len(s.events) > auditRingMax {
		s.events = s.events[len(s.events)-auditRingMax:]
	}
	return nil
}

func (s *auditEvents) Query(_ context.Context, q store.AuditQuery) ([]*model.AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	matched := make([]*model.AuditEvent, 0)
	// Newest first: walk the ring backwards.
	for i := len(s.events) - 1; i >= 0; i-- {
		e := s.events[i]
		if q.From != nil && e.Time.Before(*q.From) {
			continue
		}
		if q.To != nil && e.Time.After(*q.To) {
			continue
		}
		if q.UserSub != "" && e.UserSub != q.UserSub {
			continue
		}
		if q.Method != "" && e.Method != q.Method {
			continue
		}
		if q.PathPrefix != "" && !strings.HasPrefix(e.Path, q.PathPrefix) {
			continue
		}
		matched = append(matched, e)
	}
	if offset >= len(matched) {
		return []*model.AuditEvent{}, nil
	}
	matched = matched[offset:]
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (s *auditEvents) DeleteOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Collect kept events into a fresh backing array so the old array
	// (up to ~5 MB at auditRingMax) can be GC'd instead of being pinned
	// by the reslice-to-zero trick (DA-M / memory.go:783 fix).
	kept := make([]*model.AuditEvent, 0, len(s.events))
	deleted := 0
	for _, e := range s.events {
		if e.Time.Before(cutoff) {
			deleted++
			continue
		}
		kept = append(kept, e)
	}
	s.events = kept
	return deleted, nil
}
