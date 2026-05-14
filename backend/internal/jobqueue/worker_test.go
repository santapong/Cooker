package jobqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitForCondition polls f every 5ms up to timeout. Returns true if
// f was ever true; false on timeout. Used in tests to avoid time.Sleep
// races without bringing in a clock-injection abstraction.
func waitForCondition(t *testing.T, timeout time.Duration, f func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return f()
}

func TestPoolRunsHandlerToCompletion(t *testing.T) {
	store := NewMemory()
	reg := NewRegistry()
	var count int32
	reg.Register("k", func(ctx context.Context, j *Job) error {
		atomic.AddInt32(&count, 1)
		return nil
	})
	pool, err := NewPool(PoolOptions{
		Store:     store,
		Registry:  reg,
		Size:      2,
		PollEvery: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	for i := 0; i < 5; i++ {
		if _, err := store.Enqueue(ctx, "k", nil, EnqueueOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if !waitForCondition(t, 2*time.Second, func() bool { return atomic.LoadInt32(&count) == 5 }) {
		t.Fatalf("count=%d want 5", atomic.LoadInt32(&count))
	}
	cancel()
	<-done
}

func TestPoolRescheduleUntilExhausted(t *testing.T) {
	store := NewMemory()
	reg := NewRegistry()
	var attempts int32
	reg.Register("k", func(ctx context.Context, j *Job) error {
		atomic.AddInt32(&attempts, 1)
		return errors.New("transient")
	})
	pool, _ := NewPool(PoolOptions{
		Store:     store,
		Registry:  reg,
		Size:      1,
		PollEvery: 5 * time.Millisecond,
		// Near-zero backoff so the test finishes promptly.
		Backoff: ExponentialBackoff{Base: 1 * time.Millisecond, Max: 5 * time.Millisecond},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	job, err := store.Enqueue(ctx, "k", nil, EnqueueOptions{MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !waitForCondition(t, 2*time.Second, func() bool {
		j, err := store.Get(ctx, job.ID)
		return err == nil && j.Status == StatusFailed
	}) {
		j, _ := store.Get(ctx, job.ID)
		t.Fatalf("job didn't terminate; final=%+v attempts=%d", j, atomic.LoadInt32(&attempts))
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts=%d want 3", got)
	}
	cancel()
	<-done
}

func TestPoolHandlerPanicRecovered(t *testing.T) {
	store := NewMemory()
	reg := NewRegistry()
	var ran int32
	reg.Register("k", func(context.Context, *Job) error {
		if atomic.AddInt32(&ran, 1) == 1 {
			panic("boom")
		}
		return nil
	})
	pool, _ := NewPool(PoolOptions{
		Store:     store,
		Registry:  reg,
		Size:      1,
		PollEvery: 5 * time.Millisecond,
		Backoff:   ExponentialBackoff{Base: 1 * time.Millisecond, Max: 5 * time.Millisecond},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	job, err := store.Enqueue(ctx, "k", nil, EnqueueOptions{MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	// After the first panic the worker should reschedule and the
	// second attempt should succeed.
	if !waitForCondition(t, 2*time.Second, func() bool {
		j, err := store.Get(ctx, job.ID)
		return err == nil && j.Status == StatusSucceeded
	}) {
		j, _ := store.Get(ctx, job.ID)
		t.Fatalf("panic-then-recover: final=%+v ran=%d", j, atomic.LoadInt32(&ran))
	}
	cancel()
	<-done
}

func TestPoolGracefulDrainOnCancel(t *testing.T) {
	store := NewMemory()
	reg := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	reg.Register("k", func(ctx context.Context, j *Job) error {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	pool, _ := NewPool(PoolOptions{
		Store:     store,
		Registry:  reg,
		Size:      1,
		PollEvery: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	if _, err := store.Enqueue(ctx, "k", nil, EnqueueOptions{}); err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	close(release)
	// Run should return cleanly once the in-flight handler finishes.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel + handler finish")
	}
}

func TestPoolNotifierWakesIdleWorker(t *testing.T) {
	store := NewMemory()
	reg := NewRegistry()
	var wg sync.WaitGroup
	wg.Add(1)
	reg.Register("k", func(context.Context, *Job) error {
		wg.Done()
		return nil
	})
	not := NewChanNotifier()
	pool, _ := NewPool(PoolOptions{
		Store:     store,
		Registry:  reg,
		Size:      1,
		// Long poll so the only way the worker picks up the job
		// promptly is via the Notifier wake-up.
		PollEvery: 30 * time.Second,
		Notifier:  not,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	// Let the worker enter its idle wait.
	time.Sleep(20 * time.Millisecond)
	if _, err := store.Enqueue(ctx, "k", nil, EnqueueOptions{}); err != nil {
		t.Fatal(err)
	}
	not.Wake()
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not run after Wake; worker may not be honouring Notifier")
	}
	cancel()
	<-done
}

func TestPoolDispatchesByKind(t *testing.T) {
	store := NewMemory()
	reg := NewRegistry()
	var alphaCount, betaCount int32
	reg.Register("alpha", func(context.Context, *Job) error { atomic.AddInt32(&alphaCount, 1); return nil })
	reg.Register("beta", func(context.Context, *Job) error { atomic.AddInt32(&betaCount, 1); return nil })
	pool, _ := NewPool(PoolOptions{
		Store:     store,
		Registry:  reg,
		Size:      2,
		PollEvery: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	for i := 0; i < 3; i++ {
		if _, err := store.Enqueue(ctx, "alpha", nil, EnqueueOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Enqueue(ctx, "beta", nil, EnqueueOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if !waitForCondition(t, 2*time.Second, func() bool {
		return atomic.LoadInt32(&alphaCount) == 3 && atomic.LoadInt32(&betaCount) == 3
	}) {
		t.Fatalf("counts: alpha=%d beta=%d", atomic.LoadInt32(&alphaCount), atomic.LoadInt32(&betaCount))
	}
	cancel()
	<-done
}

// Smoke test: an enqueued payload round-trips through the registry
// untouched, so handlers can decode it without further plumbing.
func TestPoolPayloadRoundTrip(t *testing.T) {
	store := NewMemory()
	reg := NewRegistry()
	type payload struct {
		Name string `json:"name"`
	}
	var (
		mu  sync.Mutex
		got payload
	)
	reg.Register("k", func(ctx context.Context, j *Job) error {
		var local payload
		if err := json.Unmarshal(j.Payload, &local); err != nil {
			return err
		}
		mu.Lock()
		got = local
		mu.Unlock()
		return nil
	})
	pool, _ := NewPool(PoolOptions{Store: store, Registry: reg, Size: 1, PollEvery: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()

	in := payload{Name: "cooker"}
	bs, _ := json.Marshal(in)
	if _, err := store.Enqueue(ctx, "k", bs, EnqueueOptions{}); err != nil {
		t.Fatal(err)
	}
	if !waitForCondition(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return got.Name == in.Name
	}) {
		mu.Lock()
		t.Fatalf("got=%+v want %+v", got, in)
	}
	cancel()
	<-done
}

// satisfies a compile-time use of fmt to keep imports lean if we
// later add more diagnostics.
var _ = fmt.Sprintf
