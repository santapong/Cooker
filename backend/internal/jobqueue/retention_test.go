package jobqueue

// DeleteOlderThan contract test (O4/D): only terminal jobs past the
// cutoff are removed — pending and running work must survive a sweep
// regardless of age, and recent terminal jobs stay for inspection.

import (
	"context"
	"testing"
	"time"
)

func TestMemory_DeleteOlderThan(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()

	enqueue := func(id string) *Job {
		t.Helper()
		j, err := m.Enqueue(ctx, "pipeline_run", nil, EnqueueOptions{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	finish := func(id string, at time.Time) {
		t.Helper()
		j, err := m.Dequeue(ctx, "w1", nil)
		if err != nil || j == nil || j.ID != id {
			t.Fatalf("dequeue %s: job=%v err=%v", id, j, err)
		}
		if err := m.Complete(ctx, "w1", id); err != nil {
			t.Fatal(err)
		}
		// Backdate the finish stamp — the sweep keys off FinishedAt.
		m.mu.Lock()
		stamped := at
		m.jobs[id].FinishedAt = &stamped
		m.mu.Unlock()
	}

	old := time.Now().UTC().Add(-40 * 24 * time.Hour)
	enqueue("done-old")
	finish("done-old", old)
	enqueue("done-recent")
	finish("done-recent", time.Now().UTC())

	// An old job currently running: untouchable by retention. Claimed
	// before pending-old exists so Dequeue (RunAt order) picks it.
	enqueue("running-old")
	if j, err := m.Dequeue(ctx, "w2", nil); err != nil || j == nil || j.ID != "running-old" {
		t.Fatalf("dequeue running-old: job=%v err=%v", j, err)
	}
	m.mu.Lock()
	m.jobs["running-old"].UpdatedAt = old
	m.mu.Unlock()

	// An old job still pending: retention must not eat queued work.
	pending := enqueue("pending-old")
	m.mu.Lock()
	m.jobs[pending.ID].CreatedAt = old
	m.jobs[pending.ID].UpdatedAt = old
	m.mu.Unlock()

	n, err := m.DeleteOlderThan(ctx, time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted count: got %d, want 1", n)
	}
	if _, err := m.Get(ctx, "done-old"); err != ErrNotFound {
		t.Errorf("done-old should be deleted; Get err = %v", err)
	}
	for _, id := range []string{"done-recent", "pending-old", "running-old"} {
		if _, err := m.Get(ctx, id); err != nil {
			t.Errorf("%s should survive the sweep; Get err = %v", id, err)
		}
	}
}
