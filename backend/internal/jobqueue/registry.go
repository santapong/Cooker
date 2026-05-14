package jobqueue

import (
	"context"
	"fmt"
	"sync"
)

// HandlerFunc is the work a worker performs once it has claimed a
// Job from the queue. The Job's Payload is opaque to the runtime;
// each registered handler is responsible for decoding it.
//
// Returning nil signals success (the worker calls Complete).
// Returning an error signals the worker to either Reschedule (when
// attempts < max_attempts) or Fail (terminal). Handlers should
// honour ctx.Done() so a graceful drain is actually graceful.
type HandlerFunc func(ctx context.Context, job *Job) error

// Registry maps Job.Kind to its handler. One Registry per Pool; the
// Pool consults it on every Dequeue. Registrations must happen
// before Pool.Run starts (boot time, single-threaded); Dispatch is
// then called concurrently from worker goroutines under RLock.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]HandlerFunc)}
}

// Register binds a handler to a Kind. Panics on duplicate Kind so
// misconfiguration fails loud at boot rather than silently winning
// the second registration. Empty kind / nil handler also panic
// because they could only originate from a programmer bug.
func (r *Registry) Register(kind string, h HandlerFunc) {
	if kind == "" {
		panic("jobqueue: Register with empty kind")
	}
	if h == nil {
		panic("jobqueue: Register with nil handler for kind " + kind)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[kind]; exists {
		panic("jobqueue: duplicate handler for kind " + kind)
	}
	r.handlers[kind] = h
}

// Kinds returns the registered kinds in arbitrary order. Workers
// pass this to Dequeue so the SQL query filters on the server side
// instead of dequeue-and-discard for jobs no worker can run.
func (r *Registry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		out = append(out, k)
	}
	return out
}

// Dispatch looks up the handler for job.Kind and runs it. The job
// must already be claimed (Status=running, LockedBy=worker). If no
// handler is registered for the kind, returns an error so the worker
// can Fail the job — a kind with no handler is a config drift bug
// that must not silently retry forever.
func (r *Registry) Dispatch(ctx context.Context, job *Job) error {
	r.mu.RLock()
	h, ok := r.handlers[job.Kind]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("jobqueue: no handler registered for kind %q", job.Kind)
	}
	return h(ctx, job)
}
