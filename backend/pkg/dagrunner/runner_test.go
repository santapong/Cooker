package dagrunner

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestDAGTopologicalSort(t *testing.T) {
	dag := NewDAG()
	dag.AddNode("build", nil)
	dag.AddNode("test", []string{"build"})
	dag.AddNode("push", []string{"test"})
	dag.AddNode("deploy", []string{"push"})

	levels, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(levels) != 4 {
		t.Fatalf("expected 4 levels, got %d", len(levels))
	}
	if levels[0][0] != "build" {
		t.Errorf("expected build at level 0, got %s", levels[0][0])
	}
}

func TestDAGParallelExecution(t *testing.T) {
	dag := NewDAG()
	dag.AddNode("build-a", nil)
	dag.AddNode("build-b", nil)
	dag.AddNode("test", []string{"build-a", "build-b"})

	levels, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(levels))
	}
	if len(levels[0]) != 2 {
		t.Errorf("expected 2 nodes at level 0 (parallel), got %d", len(levels[0]))
	}
}

func TestDAGCycleDetection(t *testing.T) {
	dag := NewDAG()
	dag.AddNode("a", []string{"c"})
	dag.AddNode("b", []string{"a"})
	dag.AddNode("c", []string{"b"})

	_, err := dag.TopologicalSort()
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestRunnerExecution(t *testing.T) {
	dag := NewDAG()
	dag.AddNode("build", nil)
	dag.AddNode("test", []string{"build"})
	dag.AddNode("deploy", []string{"test"})

	var mu sync.Mutex
	order := []string{}

	runner := NewRunner(dag, func(ctx context.Context, nodeID string) error {
		mu.Lock()
		order = append(order, nodeID)
		mu.Unlock()
		return nil
	})

	// Drain updates in background
	go func() {
		for range runner.Updates() {
		}
	}()

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 nodes executed, got %d", len(order))
	}
	if order[0] != "build" || order[1] != "test" || order[2] != "deploy" {
		t.Errorf("unexpected execution order: %v", order)
	}
}

func TestRunnerFailure(t *testing.T) {
	dag := NewDAG()
	dag.AddNode("build", nil)
	dag.AddNode("test", []string{"build"})

	runner := NewRunner(dag, func(ctx context.Context, nodeID string) error {
		if nodeID == "build" {
			return fmt.Errorf("build failed")
		}
		return nil
	})

	go func() {
		for range runner.Updates() {
		}
	}()

	err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from failed node")
	}
}
