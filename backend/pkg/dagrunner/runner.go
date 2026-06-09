package dagrunner

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// TaskFunc is the function executed for each node. It receives the node ID and returns an error.
type TaskFunc func(ctx context.Context, nodeID string) error

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

	// Per-Run state, hoisted out of the level loop (P26-05-04):
	//
	// errCh holds the first error of a level; later errors are dropped
	// via the non-blocking send in fail() — Run only ever returned the
	// first error it drained anyway. It is drained between levels, so
	// one capacity-1 channel serves the whole Run.
	//
	// The OTel carrier captures the parent span once — ctx doesn't
	// change between levels. The fan-out semaphore likewise carries no
	// per-level state: each level joins via wg.Wait, so all slots are
	// free when the next level starts.
	errCh := make(chan error, 1)
	fail := func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	var sem chan struct{}
	if r.maxParallel > 0 {
		sem = make(chan struct{}, r.maxParallel)
	}

	for _, level := range levels {
		if err := ctx.Err(); err != nil {
			return err
		}

		var wg sync.WaitGroup
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
						fail(ctx.Err())
						return
					}
				}
				defer func() {
					if rec := recover(); rec != nil {
						err := fmt.Errorf("node %s panic: %v", id, rec)
						r.emitStatus(id, "failed", err)
						fail(err)
					}
				}()

				stageCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)
				r.emitStatus(id, "running", nil)

				if taskErr := r.taskFunc(stageCtx, id); taskErr != nil {
					r.emitStatus(id, "failed", taskErr)
					fail(fmt.Errorf("node %s failed: %w", id, taskErr))
					return
				}

				r.emitStatus(id, "success", nil)
			}(nodeID)
		}

		wg.Wait()

		select {
		case e := <-errCh:
			return e
		default:
		}
	}

	return nil
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
