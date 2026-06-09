package dagrunner

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// TaskFunc is the function executed for each node. It receives the node ID and returns an error.
type TaskFunc func(ctx context.Context, nodeID string) error

// ErrSkipped is the sentinel a TaskFunc returns to report "this node
// was deliberately not run" (edge-condition evaluation, Primitive #2).
// The runner records the node as skipped — not failed — and it does
// not abort the run.
var ErrSkipped = errors.New("dagrunner: node skipped")

// StatusUpdate is emitted when a node's status changes.
type StatusUpdate struct {
	NodeID string
	Status string // "pending", "running", "success", "failed"
	Error  error
}

// Runner executes a DAG with parallel execution of independent nodes.
type Runner struct {
	dag         *DAG
	taskFunc    TaskFunc
	updates     chan StatusUpdate
	mu          sync.Mutex
	statuses    map[string]string
	maxParallel int // 0 means unbounded (legacy behaviour)
	// continueOnError keeps walking the remaining levels after a node
	// fails (recording the first error to return at the end) instead
	// of aborting at the level barrier. Required for edge conditions:
	// a "failure"-edge downstream must still get its chance to run.
	// Per-level ctx cancellation still aborts.
	continueOnError bool
}

// NewRunner creates a new DAG runner with unbounded fan-out per
// level. For pipelines with high fan-out (100+ stages in one
// level), prefer NewRunnerBounded.
func NewRunner(dag *DAG, taskFunc TaskFunc) *Runner {
	return NewRunnerBounded(dag, taskFunc, 0)
}

// NewRunnerBounded caps the number of nodes the runner executes in
// parallel within a single level via a semaphore. maxParallel<=0
// means unbounded (matches the legacy NewRunner behaviour). A
// realistic prod default is 16 — enough for typical pipelines, low
// enough that 100 indep stages don't saturate the K8s API or burn
// every registry rate-limit budget at once.
func NewRunnerBounded(dag *DAG, taskFunc TaskFunc, maxParallel int) *Runner {
	statuses := make(map[string]string)
	for id := range dag.Nodes {
		statuses[id] = "pending"
	}
	return &Runner{
		dag:         dag,
		taskFunc:    taskFunc,
		updates:     make(chan StatusUpdate, 100),
		statuses:    statuses,
		maxParallel: maxParallel,
	}
}

// NewRunnerBoundedContinue is NewRunnerBounded with continue-through-
// failure semantics: a failed node no longer aborts at its level
// barrier; remaining levels still execute (so failure-edge downstreams
// get evaluated) and Run returns the first error at the end. Context
// cancellation still aborts between levels.
func NewRunnerBoundedContinue(dag *DAG, taskFunc TaskFunc, maxParallel int) *Runner {
	r := NewRunnerBounded(dag, taskFunc, maxParallel)
	r.continueOnError = true
	return r
}

// Updates returns a channel that receives status updates during execution.
func (r *Runner) Updates() <-chan StatusUpdate {
	return r.updates
}

// Run executes the DAG. Nodes at the same level run in parallel.
func (r *Runner) Run(ctx context.Context) error {
	defer close(r.updates)

	levels, err := r.dag.TopologicalSort()
	if err != nil {
		return fmt.Errorf("invalid DAG: %w", err)
	}

	// First node failure, returned at the end in continue mode (the
	// abort-on-first-error mode returns it from inside the loop).
	var firstErr error
	for _, level := range levels {
		if err := ctx.Err(); err != nil {
			if firstErr != nil {
				return firstErr
			}
			return err
		}

		var wg sync.WaitGroup
		errCh := make(chan error, len(level))

		// Capture the parent's OpenTelemetry span context as a TextMap
		// carrier; each goroutine extracts it back into its own ctx so
		// builder/pusher/deployer adapters running concurrently still
		// link to the right trace.
		carrier := propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(ctx, carrier)

		// Semaphore for bounded fan-out. nil when maxParallel<=0
		// preserves the legacy unbounded behaviour with a noop
		// acquire/release.
		var sem chan struct{}
		if r.maxParallel > 0 {
			sem = make(chan struct{}, r.maxParallel)
		}

		for _, nodeID := range level {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				if sem != nil {
					select {
					case sem <- struct{}{}:
						defer func() { <-sem }()
					case <-ctx.Done():
						r.emitStatus(id, "failed", ctx.Err())
						errCh <- ctx.Err()
						return
					}
				}
				defer func() {
					if rec := recover(); rec != nil {
						err := fmt.Errorf("node %s panic: %v", id, rec)
						r.emitStatus(id, "failed", err)
						errCh <- err
					}
				}()

				stageCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)
				r.emitStatus(id, "running", nil)

				if taskErr := r.taskFunc(stageCtx, id); taskErr != nil {
					if errors.Is(taskErr, ErrSkipped) {
						// Deliberate no-run, not a failure: record and
						// keep the level (and run) going.
						r.emitStatus(id, "skipped", nil)
						return
					}
					r.emitStatus(id, "failed", taskErr)
					errCh <- fmt.Errorf("node %s failed: %w", id, taskErr)
					return
				}

				r.emitStatus(id, "success", nil)
			}(nodeID)
		}

		wg.Wait()
		close(errCh)

		for e := range errCh {
			if e == nil {
				continue
			}
			if !r.continueOnError {
				return e
			}
			if firstErr == nil {
				firstErr = e
			}
		}
	}

	return firstErr
}

func (r *Runner) emitStatus(nodeID, status string, err error) {
	r.mu.Lock()
	r.statuses[nodeID] = status
	r.mu.Unlock()

	r.updates <- StatusUpdate{
		NodeID: nodeID,
		Status: status,
		Error:  err,
	}
}
