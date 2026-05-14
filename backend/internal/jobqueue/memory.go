package jobqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Memory is an in-process Store. It's used for unit tests and as a
// fallback when no database is configured. Concurrency-key semantics
// match the Postgres implementation so tests are meaningful.
type Memory struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

// NewMemory returns a fresh in-memory Store.
func NewMemory() *Memory {
	return &Memory{jobs: make(map[string]*Job)}
}

// Enqueue implements Store.
func (m *Memory) Enqueue(_ context.Context, kind string, payload json.RawMessage, opts EnqueueOptions) (*Job, error) {
	if kind == "" {
		return nil, fmt.Errorf("jobqueue: empty kind")
	}
	now := time.Now().UTC()
	runAt := opts.RunAt
	if runAt.IsZero() {
		runAt = now
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	id := opts.ID
	if id == "" {
		id = newID()
	}
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	j := &Job{
		ID:             id,
		Kind:           kind,
		Payload:        payload,
		Status:         StatusPending,
		MaxAttempts:    maxAttempts,
		RunAt:          runAt,
		ConcurrencyKey: opts.ConcurrencyKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[id]; ok {
		return nil, fmt.Errorf("jobqueue: duplicate job id %s", id)
	}
	m.jobs[id] = j
	return cloneJob(j), nil
}

// Dequeue implements Store. Scans pending jobs in run_at order,
// skipping ones whose concurrency_key is already held by a running
// job. Returns (nil, nil) when nothing is ready.
func (m *Memory) Dequeue(_ context.Context, workerID string, kinds []string) (*Job, error) {
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()

	runningKeys := make(map[string]bool)
	for _, j := range m.jobs {
		if j.Status == StatusRunning && j.ConcurrencyKey != "" {
			runningKeys[j.ConcurrencyKey] = true
		}
	}

	kindFilter := toSet(kinds)

	var candidates []*Job
	for _, j := range m.jobs {
		if j.Status != StatusPending {
			continue
		}
		if j.RunAt.After(now) {
			continue
		}
		if len(kindFilter) > 0 && !kindFilter[j.Kind] {
			continue
		}
		if j.ConcurrencyKey != "" && runningKeys[j.ConcurrencyKey] {
			continue
		}
		candidates = append(candidates, j)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.Slice(candidates, func(i, k int) bool {
		return candidates[i].RunAt.Before(candidates[k].RunAt)
	})
	picked := candidates[0]
	picked.Status = StatusRunning
	picked.LockedBy = workerID
	t := now
	picked.LockedAt = &t
	picked.StartedAt = &t
	picked.Attempts++
	picked.UpdatedAt = now
	return cloneJob(picked), nil
}

func (m *Memory) Complete(_ context.Context, workerID, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return ErrNotFound
	}
	if j.LockedBy != workerID || j.Status != StatusRunning {
		return ErrNotLocked
	}
	now := time.Now().UTC()
	j.Status = StatusSucceeded
	j.FinishedAt = &now
	j.LockedBy = ""
	j.UpdatedAt = now
	return nil
}

func (m *Memory) Fail(_ context.Context, workerID, jobID string, lastError string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return ErrNotFound
	}
	if j.LockedBy != workerID || j.Status != StatusRunning {
		return ErrNotLocked
	}
	now := time.Now().UTC()
	j.Status = StatusFailed
	j.FinishedAt = &now
	j.LastError = lastError
	j.LockedBy = ""
	j.UpdatedAt = now
	return nil
}

func (m *Memory) Reschedule(_ context.Context, workerID, jobID string, nextRunAt time.Time, lastError string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return ErrNotFound
	}
	if j.LockedBy != workerID || j.Status != StatusRunning {
		return ErrNotLocked
	}
	now := time.Now().UTC()
	// If we've exhausted attempts, transition to failed terminal
	// state instead of returning to pending. The worker treats this
	// path identically to Fail at the call site.
	if j.Attempts >= j.MaxAttempts {
		j.Status = StatusFailed
		j.FinishedAt = &now
		j.LastError = lastError
		j.LockedBy = ""
		j.UpdatedAt = now
		return nil
	}
	j.Status = StatusPending
	j.RunAt = nextRunAt
	j.LastError = lastError
	j.LockedBy = ""
	j.LockedAt = nil
	j.StartedAt = nil
	j.UpdatedAt = now
	return nil
}

func (m *Memory) Get(_ context.Context, jobID string) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneJob(j), nil
}

func (m *Memory) Stats(_ context.Context) (map[Status]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[Status]int)
	for _, j := range m.jobs {
		out[j.Status]++
	}
	return out, nil
}

func cloneJob(j *Job) *Job {
	cp := *j
	if j.LockedAt != nil {
		t := *j.LockedAt
		cp.LockedAt = &t
	}
	if j.StartedAt != nil {
		t := *j.StartedAt
		cp.StartedAt = &t
	}
	if j.FinishedAt != nil {
		t := *j.FinishedAt
		cp.FinishedAt = &t
	}
	if len(j.Payload) > 0 {
		cp.Payload = append(json.RawMessage(nil), j.Payload...)
	}
	return &cp
}

func toSet(s []string) map[string]bool {
	if len(s) == 0 {
		return nil
	}
	out := make(map[string]bool, len(s))
	for _, v := range s {
		if v != "" {
			out[v] = true
		}
	}
	return out
}
