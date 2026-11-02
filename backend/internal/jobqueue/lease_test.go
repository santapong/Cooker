package jobqueue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExpiredLeaseRequiresReconciliationAndFencesOldWorker(t *testing.T) {
	ctx := context.Background()
	s := NewMemory()
	first, _ := s.Enqueue(ctx, "deploy", nil, EnqueueOptions{ConcurrencyKey: "app:one"})
	_, _ = s.Enqueue(ctx, "deploy", nil, EnqueueOptions{ConcurrencyKey: "app:one"})
	claimed, err := s.Dequeue(ctx, "old-claim", nil)
	if err != nil || claimed.ID != first.ID {
		t.Fatal(claimed, err)
	}
	jobs, err := s.RecoverExpired(ctx, time.Now().Add(time.Second))
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	if err := s.Complete(ctx, "old-claim", first.ID); !errors.Is(err, ErrNotLocked) {
		t.Fatal(err)
	}
	if err := s.Heartbeat(ctx, "old-claim", first.ID); !errors.Is(err, ErrNotLocked) {
		t.Fatal(err)
	}
	if j, err := s.Dequeue(ctx, "next", nil); j != nil || err != nil {
		t.Fatal(j, err)
	}
	if n, err := s.DeleteOlderThan(ctx, time.Now().Add(time.Hour)); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if err := s.Resolve(ctx, first.ID, StatusPending, "retry blindly"); err == nil {
		t.Fatal("must not automatically replay")
	}
	if err := s.Resolve(ctx, first.ID, StatusSucceeded, "inspected target revision and health"); err != nil {
		t.Fatal(err)
	}
	if j, err := s.Dequeue(ctx, "new-claim", nil); j == nil || err != nil {
		t.Fatal(j, err)
	}
}

func TestHeartbeatProtectsLiveClaim(t *testing.T) {
	ctx := context.Background()
	s := NewMemory()
	j, _ := s.Enqueue(ctx, "deploy", nil, EnqueueOptions{})
	_, _ = s.Dequeue(ctx, "owner", nil)
	cutoff := time.Now()
	if err := s.Heartbeat(ctx, "owner", j.ID); err != nil {
		t.Fatal(err)
	}
	if recovered, err := s.RecoverExpired(ctx, cutoff); len(recovered) != 0 || err != nil {
		t.Fatal(recovered, err)
	}
}

func TestCancellationDistinguishesPendingAndRunningWork(t *testing.T) {
	ctx := context.Background()
	s := NewMemory()
	pending, _ := s.Enqueue(ctx, "deploy", nil, EnqueueOptions{})
	status, err := s.Cancel(ctx, pending.ID)
	if err != nil || status != StatusCancelled {
		t.Fatal(status, err)
	}
	if j, err := s.Dequeue(ctx, "worker", nil); j != nil || err != nil {
		t.Fatal(j, err)
	}
	running, _ := s.Enqueue(ctx, "deploy", nil, EnqueueOptions{ConcurrencyKey: "app"})
	_, _ = s.Dequeue(ctx, "owner", nil)
	status, err = s.Cancel(ctx, running.ID)
	if err != nil || status != StatusNeedsAttention {
		t.Fatal(status, err)
	}
	if err := s.Heartbeat(ctx, "owner", running.ID); !errors.Is(err, ErrNotLocked) {
		t.Fatal(err)
	}
}

func TestPoolShutdownDoesNotReplayExternalWork(t *testing.T) {
	s := NewMemory()
	reg := NewRegistry()
	started := make(chan struct{})
	reg.Register("deploy", func(ctx context.Context, j *Job) error { close(started); <-ctx.Done(); return ctx.Err() })
	j, _ := s.Enqueue(context.Background(), "deploy", nil, EnqueueOptions{MaxAttempts: 3})
	p, _ := NewPool(PoolOptions{Store: s, Registry: reg, Size: 1, PollEvery: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = p.Run(ctx); close(done) }()
	<-started
	cancel()
	<-done
	got, _ := s.Get(context.Background(), j.ID)
	if got.Status != StatusNeedsAttention || got.Attempts != 1 {
		t.Fatalf("unsafe shutdown outcome: %+v", got)
	}
}
