package dagrunner

import (
	"context"
	"fmt"
	"sync"
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
	dag      *DAG
	taskFunc TaskFunc
	updates  chan StatusUpdate
	mu       sync.Mutex
	statuses map[string]string
}

// NewRunner creates a new DAG runner.
func NewRunner(dag *DAG, taskFunc TaskFunc) *Runner {
	statuses := make(map[string]string)
	for id := range dag.Nodes {
		statuses[id] = "pending"
	}
	return &Runner{
		dag:      dag,
		taskFunc: taskFunc,
		updates:  make(chan StatusUpdate, 100),
		statuses: statuses,
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

	for _, level := range levels {
		if err := ctx.Err(); err != nil {
			return err
		}

		var wg sync.WaitGroup
		errCh := make(chan error, len(level))

		for _, nodeID := range level {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				defer func() {
					if rec := recover(); rec != nil {
						err := fmt.Errorf("node %s panic: %v", id, rec)
						r.emitStatus(id, "failed", err)
						errCh <- err
					}
				}()

				r.emitStatus(id, "running", nil)

				if taskErr := r.taskFunc(ctx, id); taskErr != nil {
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
			if e != nil {
				return e
			}
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
