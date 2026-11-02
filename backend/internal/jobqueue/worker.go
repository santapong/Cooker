package jobqueue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/observability"
)

// PoolOptions configures a Pool. All fields are optional except
// Store and Registry. Zero-valued fields fall back to sensible
// defaults documented per field.
type PoolOptions struct {
	Store    Store
	Registry *Registry
	// Size is the worker count. Default 4. Tuning guidance: roughly
	// match the maximum number of concurrent pipeline runs the
	// operator wants to permit; ConcurrencyKey serialises per-key
	// anyway, so over-provisioning is safe.
	Size int
	// PollEvery is the fallback poll interval when no NOTIFY arrives.
	// Default 5s. A short value tightens response time for jobs
	// scheduled via run_at (cron-like); a long value reduces idle
	// queries on a quiet system. NOTIFY wakeups bypass this entirely.
	PollEvery time.Duration
	// Backoff overrides the default exponential policy. Zero uses
	// DefaultBackoff.
	Backoff BackoffPolicy
	// Notifier, when non-nil, lets workers block on NOTIFY events
	// for near-instant wake-up. nil means "poll only".
	Notifier Notifier
	// WorkerIDPrefix is prepended to each worker's identifier (used
	// as locked_by). Default "cooker". Operators running multiple
	// replicas should set this to a host- or pod-identifier so
	// in-flight job rows show which replica owns them.
	WorkerIDPrefix string
	// LeaseTimeout is the maximum age of an unrenewed claim. Default two minutes.
	LeaseTimeout   time.Duration
	HeartbeatEvery time.Duration
	// OnHeartbeat updates the corresponding domain run, if any.
	OnHeartbeat func(context.Context, *Job) error
	// OnRecovery records ambiguous external outcomes in the domain run.
	OnRecovery func(context.Context, *Job) error
	// ClaimContext carries the domain store's fenced-write context.
	ClaimContext func(context.Context, *Job) context.Context
	// OnComplete emits best-effort notifications only after durable completion.
	OnComplete func(context.Context, *Job) error
}

// Pool is N worker goroutines polling the same Store.
type Pool struct {
	opts PoolOptions
	wg   sync.WaitGroup
}

// NewPool builds a Pool. Returns an error only on programmer
// mistakes (nil Store / Registry) so callers can surface them at
// boot rather than at the first Dequeue.
func NewPool(opts PoolOptions) (*Pool, error) {
	if opts.Store == nil {
		return nil, errors.New("jobqueue: PoolOptions.Store is nil")
	}
	if opts.Registry == nil {
		return nil, errors.New("jobqueue: PoolOptions.Registry is nil")
	}
	if opts.Size <= 0 {
		opts.Size = 4
	}
	if opts.PollEvery <= 0 {
		opts.PollEvery = 5 * time.Second
	}
	if opts.Backoff == nil {
		opts.Backoff = DefaultBackoff
	}
	if opts.WorkerIDPrefix == "" {
		opts.WorkerIDPrefix = "cooker"
	}
	if opts.LeaseTimeout <= 0 {
		opts.LeaseTimeout = 2 * time.Minute
	}
	if opts.HeartbeatEvery <= 0 {
		opts.HeartbeatEvery = opts.LeaseTimeout / 4
	}
	if opts.HeartbeatEvery >= opts.LeaseTimeout/2 {
		return nil, errors.New("jobqueue: heartbeat must be less than half the lease timeout")
	}
	return &Pool{opts: opts}, nil
}

// Run starts the worker goroutines and blocks until ctx is
// cancelled. Workers in flight complete their current job before
// exiting; new jobs are not claimed once ctx is done.
func (p *Pool) Run(ctx context.Context) error {
	kinds := p.opts.Registry.Kinds()
	if leases, ok := p.opts.Store.(LeaseStore); ok {
		p.wg.Add(1)
		go p.recover(ctx, leases)
	}
	slog.Info("jobqueue: starting worker pool",
		"size", p.opts.Size,
		"poll_every", p.opts.PollEvery.String(),
		"kinds", kinds)
	for i := 0; i < p.opts.Size; i++ {
		workerID := fmt.Sprintf("%s-w%d", p.opts.WorkerIDPrefix, i)
		p.wg.Add(1)
		go p.run(ctx, workerID, kinds)
	}
	p.wg.Wait()
	slog.Info("jobqueue: worker pool drained")
	return nil
}

// run is one worker goroutine's main loop. Sleeps on NOTIFY (or a
// timer fallback) when there's no work; runs the registered handler
// when it claims a job; reports the outcome back to the Store.
func (p *Pool) run(ctx context.Context, workerID string, kinds []string) {
	defer p.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		claimID := workerID + "-" + uuid.NewString()
		job, err := p.opts.Store.Dequeue(ctx, claimID, kinds)
		if err != nil {
			slog.Warn("jobqueue: dequeue failed", "worker", workerID, "err", err)
			// Brief sleep to avoid a hot-loop on a persistent
			// connection failure. The next iteration retries.
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		if job == nil {
			// No work — wait for NOTIFY or the poll fallback.
			if p.opts.Notifier != nil {
				p.opts.Notifier.Wait(ctx, p.opts.PollEvery)
			} else {
				select {
				case <-ctx.Done():
					return
				case <-time.After(p.opts.PollEvery):
				}
			}
			continue
		}
		p.handle(ctx, claimID, job)
	}
}

// handle dispatches a single claimed job and reports the outcome.
// Panics in handlers are recovered so a buggy handler can't crash
// the worker goroutine; the panic is logged, the stack captured,
// and the job is treated as if the handler returned an error.
func (p *Pool) handle(ctx context.Context, workerID string, job *Job) {
	start := time.Now()
	if p.opts.ClaimContext != nil {
		ctx = p.opts.ClaimContext(ctx, job)
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopped := make(chan struct{})
	heartbeatDone := make(chan struct{})
	var leaseErr error
	if leases, ok := p.opts.Store.(LeaseStore); ok {
		go func() {
			defer close(heartbeatDone)
			ticker := time.NewTicker(p.opts.HeartbeatEvery)
			defer ticker.Stop()
			beat := func() error {
				beatCtx, stop := context.WithTimeout(workCtx, p.opts.HeartbeatEvery)
				defer stop()
				if err := leases.Heartbeat(beatCtx, workerID, job.ID); err != nil {
					return err
				}
				if p.opts.OnHeartbeat != nil {
					return p.opts.OnHeartbeat(beatCtx, job)
				}
				return nil
			}
			for {
				if err := beat(); err != nil {
					leaseErr = err
					cancel()
					return
				}
				select {
				case <-stopped:
					return
				case <-workCtx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	} else {
		close(heartbeatDone)
	}
	var handlerErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				handlerErr = fmt.Errorf("%w: handler panic", ErrOutcomeUnknown)
				slog.Error("jobqueue: handler panic recovered",
					"worker", workerID,
					"kind", job.Kind,
					"job", job.ID,
					"stack", string(debug.Stack()))
			}
		}()
		handlerErr = p.opts.Registry.Dispatch(workCtx, job)
	}()

	close(stopped)
	<-heartbeatDone
	// Recording the outcome must remain possible during graceful shutdown.
	recordCtx, recordCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer recordCancel()
	if leaseErr != nil || ctx.Err() != nil || errors.Is(handlerErr, ErrOutcomeUnknown) {
		if leases, ok := p.opts.Store.(LeaseStore); ok {
			reason := "execution interrupted; inspect the external deployment before recording an outcome"
			if err := leases.Suspend(recordCtx, workerID, job.ID, reason); err == nil {
				job.Status, job.LastError = StatusNeedsAttention, reason
				p.notifyRecovery(recordCtx, job)
			} else {
				slog.Warn("jobqueue: suspend failed", "job", job.ID, "err", err)
			}
			return
		}
	}
	dur := time.Since(start)
	observability.IncJobqueueAttempts(job.Kind)
	observability.ObserveJobqueueDuration(job.Kind, dur, handlerErr == nil)

	if handlerErr == nil {
		if err := p.opts.Store.Complete(recordCtx, workerID, job.ID); err != nil {
			slog.Warn("jobqueue: complete failed",
				"worker", workerID, "job", job.ID, "err", err)
		} else if p.opts.OnComplete != nil {
			if err := p.opts.OnComplete(recordCtx, job); err != nil {
				slog.Warn("jobqueue: completion notification failed", "job", job.ID, "err", err)
			}
		}
		return
	}

	// Failure path: either reschedule (attempts < max_attempts) or
	// terminal fail. The Store's Reschedule transitions to 'failed'
	// atomically when attempts hit the cap, so callers don't have to
	// branch — we always call Reschedule on error and let it pick.
	nextRunAt := time.Now().Add(p.opts.Backoff.NextDelay(job.Attempts))
	if err := p.opts.Store.Reschedule(recordCtx, workerID, job.ID, nextRunAt, handlerErr.Error()); err != nil {
		slog.Warn("jobqueue: reschedule failed",
			"worker", workerID, "job", job.ID, "err", err)
	}
}

func (p *Pool) notifyRecovery(ctx context.Context, job *Job) {
	if atomic, ok := p.opts.Store.(interface{ AtomicLifecycle() bool }); ok && atomic.AtomicLifecycle() {
		return
	}
	if p.opts.OnRecovery != nil {
		if err := p.opts.OnRecovery(ctx, job); err != nil {
			slog.Warn("jobqueue: domain recovery update failed", "job", job.ID, "err", err)
		}
	}
}

func (p *Pool) recover(ctx context.Context, leases LeaseStore) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.opts.HeartbeatEvery)
	defer ticker.Stop()
	for {
		sweepCtx, cancel := context.WithTimeout(ctx, p.opts.HeartbeatEvery)
		jobs, err := leases.RecoverExpired(sweepCtx, time.Now().Add(-p.opts.LeaseTimeout))
		if err != nil && ctx.Err() == nil {
			slog.Warn("jobqueue: lease recovery failed", "err", err)
		}
		for _, job := range jobs {
			p.notifyRecovery(sweepCtx, job)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
