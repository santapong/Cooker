package neon

import (
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryConnections struct {
	mu      sync.Mutex
	records map[string]*ConnectionRecord
}

func NewMemoryConnections() *MemoryConnections {
	return &MemoryConnections{records: map[string]*ConnectionRecord{}}
}

func (m *MemoryConnections) List(ctx context.Context) ([]*ConnectionRecord, error) {
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.records) > MaxNeonConnections {
		return nil, connectionError("connection_limit")
	}
	out := make([]*ConnectionRecord, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, cloneConnectionRecord(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryConnections) Get(ctx context.Context, id string) (*ConnectionRecord, error) {
	if ctx.Err() != nil {
		return nil, connectionError("canceled")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return nil, connectionError("not_found")
	}
	return cloneConnectionRecord(r), nil
}

func (m *MemoryConnections) Create(ctx context.Context, r *ConnectionRecord) error {
	if !validConnectionRecord(r) || r.Version != 1 {
		return connectionError("invalid_input")
	}
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	if _, exists := m.records[r.ID]; exists {
		return connectionError("conflict")
	}
	if len(m.records) >= MaxNeonConnections {
		return connectionError("connection_limit")
	}
	m.records[r.ID] = cloneConnectionRecord(r)
	return nil
}

func (m *MemoryConnections) Update(ctx context.Context, r *ConnectionRecord, version int) error {
	if !validConnectionRecord(r) || r.Version != version {
		return connectionError("invalid_input")
	}
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	current, ok := m.records[r.ID]
	if !ok {
		return connectionError("not_found")
	}
	if current.Version != version || current.OrganizationID != r.OrganizationID {
		return connectionError("conflict")
	}
	next := applyConnectionConfig(current, r)
	m.records[r.ID] = next
	r.Version = next.Version
	return nil
}

func (m *MemoryConnections) Delete(ctx context.Context, id string, version int) error {
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	r, ok := m.records[id]
	if !ok {
		return connectionError("not_found")
	}
	if r.Version != version {
		return connectionError("conflict")
	}
	delete(m.records, id)
	return nil
}

func (m *MemoryConnections) Claim(ctx context.Context, id string, version int, lease string, now, until time.Time) error {
	if !validConnectionID(lease) || now.IsZero() || !until.After(now) || until.Sub(now) > ConnectionLeaseDuration {
		return connectionError("invalid_input")
	}
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	r, ok := m.records[id]
	if !ok {
		return connectionError("not_found")
	}
	if r.Version != version {
		return connectionError("conflict")
	}
	if !r.Enabled || len(r.ProjectIDs) == 0 {
		return connectionError("paused")
	}
	if r.Lease != "" && r.LeaseUntil != nil && r.LeaseUntil.After(now) {
		return connectionError("busy")
	}
	r.Lease, r.LeaseUntil, r.LastAttemptAt = lease, &until, &now
	return nil
}

func (m *MemoryConnections) Finish(ctx context.Context, id string, version int, lease string, snapshot *ConnectionSnapshot, code string, now time.Time) error {
	if now.IsZero() || (code != "" && !safeConnectionCode(code)) || (snapshot == nil && code == "") || (snapshot != nil && code != "") {
		return connectionError("invalid_input")
	}
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return connectionError("canceled")
	}
	r, ok := m.records[id]
	if !ok {
		return connectionError("not_found")
	}
	if r.Version != version || r.Lease != lease || r.LeaseUntil == nil || !r.LeaseUntil.After(now) || r.LastAttemptAt == nil || now.Before(*r.LastAttemptAt) {
		return connectionError("conflict")
	}
	if snapshot != nil {
		if validateConnectionSnapshot(snapshot, r.OrganizationID, r.ProjectIDs) != nil {
			return connectionError("invalid_response")
		}
		r.Snapshot = cloneConnectionSnapshot(snapshot)
		r.Snapshot.ObservedAt = now
		r.LastSuccessAt = &now
	}
	r.ErrorCode, r.Lease, r.LeaseUntil = code, "", nil
	return nil
}
