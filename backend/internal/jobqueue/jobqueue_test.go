package jobqueue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestStatusIsTerminal(t *testing.T) {
	cases := map[Status]bool{
		StatusPending:   false,
		StatusRunning:   false,
		StatusSucceeded: true,
		StatusFailed:    true,
		StatusCancelled: true,
	}
	for s, want := range cases {
		if got := s.IsTerminal(); got != want {
			t.Errorf("%s: IsTerminal=%v want %v", s, got, want)
		}
	}
}

func TestMemoryEnqueueDequeueComplete(t *testing.T) {
	ctx := context.Background()
	q := NewMemory()

	enqueued, err := q.Enqueue(ctx, "pipeline-run", json.RawMessage(`{"runId":"r1"}`), EnqueueOptions{})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if enqueued.Status != StatusPending {
		t.Fatalf("new job status=%s want pending", enqueued.Status)
	}
	if enqueued.MaxAttempts != 1 {
		t.Fatalf("default MaxAttempts=%d want 1", enqueued.MaxAttempts)
	}

	claimed, err := q.Dequeue(ctx, "worker-1", nil)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if claimed == nil {
		t.Fatal("Dequeue: got nil, want job")
	}
	if claimed.ID != enqueued.ID || claimed.Status != StatusRunning || claimed.LockedBy != "worker-1" {
		t.Fatalf("claimed=%+v", claimed)
	}
	if claimed.Attempts != 1 {
		t.Fatalf("claimed.Attempts=%d want 1", claimed.Attempts)
	}

	if err := q.Complete(ctx, "worker-1", claimed.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	final, err := q.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != StatusSucceeded {
		t.Fatalf("final.Status=%s want succeeded", final.Status)
	}
	if final.FinishedAt == nil {
		t.Fatal("FinishedAt nil after Complete")
	}
}

func TestMemoryDequeueEmptyWhenNoWork(t *testing.T) {
	ctx := context.Background()
	q := NewMemory()
	got, err := q.Dequeue(ctx, "w", nil)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestMemoryDequeueRespectsRunAt(t *testing.T) {
	ctx := context.Background()
	q := NewMemory()
	future := time.Now().Add(1 * time.Hour)
	if _, err := q.Enqueue(ctx, "k", nil, EnqueueOptions{RunAt: future}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got, err := q.Dequeue(ctx, "w", nil)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got != nil {
		t.Fatalf("future job should not be runnable yet, got %+v", got)
	}
}

func TestMemoryDequeueKindFilter(t *testing.T) {
	ctx := context.Background()
	q := NewMemory()
	if _, err := q.Enqueue(ctx, "alpha", nil, EnqueueOptions{}); err != nil {
		t.Fatal(err)
	}
	beta, err := q.Enqueue(ctx, "beta", nil, EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Dequeue(ctx, "w", []string{"beta"})
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got == nil || got.ID != beta.ID {
		t.Fatalf("want beta, got %+v", got)
	}
}

func TestMemoryConcurrencyKeySerialisesRuns(t *testing.T) {
	ctx := context.Background()
	q := NewMemory()
	// Two jobs with the same concurrency_key. The first Dequeue
	// should claim job A; the second Dequeue (while A is running)
	// must return nil because the key is held. After A completes,
	// B becomes available.
	a, err := q.Enqueue(ctx, "deploy", nil, EnqueueOptions{ConcurrencyKey: "pipeline-1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.Enqueue(ctx, "deploy", nil, EnqueueOptions{ConcurrencyKey: "pipeline-1"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := q.Dequeue(ctx, "w1", nil)
	if err != nil {
		t.Fatalf("first Dequeue: %v", err)
	}
	if first == nil || first.ID != a.ID {
		t.Fatalf("first claim: want %s, got %+v", a.ID, first)
	}
	second, err := q.Dequeue(ctx, "w2", nil)
	if err != nil {
		t.Fatalf("second Dequeue: %v", err)
	}
	if second != nil {
		t.Fatalf("second Dequeue should be blocked by concurrency_key, got %+v", second)
	}
	if err := q.Complete(ctx, "w1", a.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	third, err := q.Dequeue(ctx, "w2", nil)
	if err != nil {
		t.Fatalf("third Dequeue: %v", err)
	}
	if third == nil || third.ID != b.ID {
		t.Fatalf("after Complete: want %s, got %+v", b.ID, third)
	}
}

func TestMemoryRescheduleUntilExhausted(t *testing.T) {
	ctx := context.Background()
	q := NewMemory()
	j, err := q.Enqueue(ctx, "k", nil, EnqueueOptions{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	// Attempt 1: claim, reschedule.
	c1, err := q.Dequeue(ctx, "w", nil)
	if err != nil || c1 == nil {
		t.Fatalf("first claim: c1=%+v err=%v", c1, err)
	}
	if err := q.Reschedule(ctx, "w", j.ID, time.Now(), "transient"); err != nil {
		t.Fatalf("Reschedule 1: %v", err)
	}
	back, err := q.Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Status != StatusPending {
		t.Fatalf("after 1st Reschedule status=%s want pending", back.Status)
	}
	if back.LastError != "transient" {
		t.Fatalf("LastError=%q want transient", back.LastError)
	}
	// Attempt 2: claim, reschedule — attempts hits MaxAttempts so
	// the row should land in failed terminal state.
	c2, err := q.Dequeue(ctx, "w", nil)
	if err != nil || c2 == nil {
		t.Fatalf("second claim: c2=%+v err=%v", c2, err)
	}
	if c2.Attempts != 2 {
		t.Fatalf("c2.Attempts=%d want 2", c2.Attempts)
	}
	if err := q.Reschedule(ctx, "w", j.ID, time.Now(), "still bad"); err != nil {
		t.Fatalf("Reschedule 2: %v", err)
	}
	final, err := q.Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != StatusFailed {
		t.Fatalf("final.Status=%s want failed", final.Status)
	}
}

func TestMemoryFailRequiresLock(t *testing.T) {
	ctx := context.Background()
	q := NewMemory()
	j, err := q.Enqueue(ctx, "k", nil, EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Fail without claiming first must surface ErrNotLocked, not a
	// silent no-op. This is the safety property that prevents a
	// stray Fail call from another worker corrupting an in-flight
	// job's terminal state.
	err = q.Fail(ctx, "unrelated-worker", j.ID, "boom")
	if !errors.Is(err, ErrNotLocked) {
		t.Fatalf("want ErrNotLocked, got %v", err)
	}
}

func TestMemoryGetNotFound(t *testing.T) {
	ctx := context.Background()
	q := NewMemory()
	_, err := q.Get(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMemoryStats(t *testing.T) {
	ctx := context.Background()
	q := NewMemory()
	for i := 0; i < 3; i++ {
		if _, err := q.Enqueue(ctx, "k", nil, EnqueueOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	// Claim one of them.
	if _, err := q.Dequeue(ctx, "w", nil); err != nil {
		t.Fatal(err)
	}
	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats[StatusPending] != 2 || stats[StatusRunning] != 1 {
		t.Fatalf("stats=%v want pending=2 running=1", stats)
	}
}

func TestPgTextArray(t *testing.T) {
	cases := map[string]struct {
		in   []string
		want string
	}{
		"empty":         {nil, "{}"},
		"one":           {[]string{"alpha"}, `{"alpha"}`},
		"two":           {[]string{"alpha", "beta"}, `{"alpha","beta"}`},
		"quote":         {[]string{`a"b`}, `{"a\"b"}`},
		"backslash":     {[]string{`a\b`}, `{"a\\b"}`},
	}
	for name, c := range cases {
		if got := pgTextArray(c.in); got != c.want {
			t.Errorf("%s: pgTextArray(%v)=%q want %q", name, c.in, got, c.want)
		}
	}
}
