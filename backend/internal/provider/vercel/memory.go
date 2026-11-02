package vercel

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Memory struct {
	mu      sync.Mutex
	records map[string]*Record
}

func NewMemory() *Memory { return &Memory{records: map[string]*Record{}} }
func clone(r *Record) *Record {
	v := *r
	v.ProjectIDs = append([]string{}, r.ProjectIDs...)
	v.Credential = append([]byte{}, r.Credential...)
	v.Integrations = cloneIntegrationInventory(r.Integrations)
	v.Snapshot = cloneSnapshot(r.Snapshot)
	return &v
}
func (m *Memory) List(_ context.Context) ([]*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*Record{}
	for _, r := range m.records {
		out = append(out, clone(r))
	}
	return out, nil
}
func (m *Memory) Get(_ context.Context, id string) (*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(r), nil
}
func (m *Memory) Create(_ context.Context, r *Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[r.ID]; ok {
		return ErrConflict
	}
	m.records[r.ID] = clone(r)
	return nil
}

// applyConfig keeps the latest sync outcome under the store's write lock.
func applyConfig(current, proposed *Record) *Record {
	out := clone(current)
	out.Name = proposed.Name
	out.Enabled = proposed.Enabled
	out.Credential = append([]byte{}, proposed.Credential...)
	out.ProjectIDs = append([]string{}, proposed.ProjectIDs...)
	out.Version++
	out.UpdatedAt = proposed.UpdatedAt
	out.Lease = ""
	out.LeaseUntil = nil
	out.NextSyncAt = &out.UpdatedAt
	if !reflect.DeepEqual(current.ProjectIDs, proposed.ProjectIDs) {
		out.Snapshot = Snapshot{Projects: []Project{}, Deployments: []Deployment{}}
		out.LastSuccessAt = nil
		out.SyncError = ""
	}
	return out
}
func (m *Memory) Update(_ context.Context, r *Record, version int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.records[r.ID]
	if !ok {
		return ErrNotFound
	}
	if old.Version != version {
		return ErrConflict
	}
	next := applyConfig(old, r)
	m.records[r.ID] = next
	r.Version = next.Version
	return nil
}
func (m *Memory) Delete(_ context.Context, id string, version int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return ErrNotFound
	}
	if r.Version != version {
		return ErrConflict
	}
	delete(m.records, id)
	return nil
}
func canClaim(r *Record, now time.Time, manual bool) bool {
	return r.Enabled && len(r.ProjectIDs) > 0 && (r.LeaseUntil == nil || !r.LeaseUntil.After(now)) && (r.RetryAt == nil || !r.RetryAt.After(now)) && (manual || r.NextSyncAt == nil || !r.NextSyncAt.After(now))
}
func (m *Memory) Claim(_ context.Context, id string, version int, now, until time.Time, manual bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return "", ErrNotFound
	}
	if r.Version != version {
		return "", ErrConflict
	}
	if !canClaim(r, now, manual) {
		return "", ErrBusy
	}
	r.Lease = uuid.NewString()
	r.LeaseUntil = &until
	r.LastAttemptAt = &now
	return r.Lease, nil
}
func complete(r *Record, snapshot *Snapshot, message string, next time.Time, retry *time.Time) {
	r.Lease = ""
	r.LeaseUntil = nil
	r.SyncError = message
	r.NextSyncAt = &next
	r.RetryAt = retry
	if snapshot != nil {
		r.Snapshot = cloneSnapshot(*snapshot)
		now := time.Now().UTC()
		r.LastSuccessAt = &now
		r.Failures = 0
	} else {
		r.Failures++
	}
}
func (m *Memory) Finish(_ context.Context, id string, version int, lease string, snapshot *Snapshot, message string, next time.Time, retry *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return ErrNotFound
	}
	if r.Version != version || r.Lease != lease || r.LeaseUntil == nil || !r.LeaseUntil.After(time.Now()) {
		return ErrConflict
	}
	complete(r, snapshot, message, next, retry)
	return nil
}
