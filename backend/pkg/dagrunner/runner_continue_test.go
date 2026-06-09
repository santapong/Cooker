package dagrunner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func drainUpdates(r *Runner) (*sync.Map, chan struct{}) {
	seen := &sync.Map{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for u := range r.Updates() {
			seen.Store(u.NodeID+":"+u.Status, true)
		}
	}()
	return seen, done
}

// ErrSkipped from a taskFunc records the node as skipped and does not
// fail the run.
func TestRunner_ErrSkippedIsNotAFailure(t *testing.T) {
	dag := NewDAG()
	dag.AddNode("a", nil)
	dag.AddNode("b", []string{"a"})

	r := NewRunnerBounded(dag, func(_ context.Context, id string) error {
		if id == "b" {
			return ErrSkipped
		}
		return nil
	}, 0)
	seen, drained := drainUpdates(r)
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-drained
	if _, ok := seen.Load("b:skipped"); !ok {
		t.Error("expected a b:skipped status update")
	}
	if _, ok := seen.Load("b:failed"); ok {
		t.Error("skipped node must not emit failed")
	}
}

// Continue mode: a failure in level 1 doesn't stop level 2; Run
// returns the first error at the end.
func TestRunner_ContinueModeWalksAllLevels(t *testing.T) {
	dag := NewDAG()
	dag.AddNode("a", nil)
	dag.AddNode("b", []string{"a"})

	var ran sync.Map
	r := NewRunnerBoundedContinue(dag, func(_ context.Context, id string) error {
		ran.Store(id, true)
		if id == "a" {
			return errors.New("boom")
		}
		return nil
	}, 0)
	go func() {
		for range r.Updates() {
		}
	}()
	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the first error back, got %v", err)
	}
	if _, ok := ran.Load("b"); !ok {
		t.Error("continue mode must still run downstream levels")
	}
}

// Legacy mode is unchanged: a failure aborts at the level barrier.
func TestRunner_DefaultModeStillAborts(t *testing.T) {
	dag := NewDAG()
	dag.AddNode("a", nil)
	dag.AddNode("b", []string{"a"})

	var ran sync.Map
	r := NewRunnerBounded(dag, func(_ context.Context, id string) error {
		ran.Store(id, true)
		if id == "a" {
			return errors.New("boom")
		}
		return nil
	}, 0)
	go func() {
		for range r.Updates() {
		}
	}()
	if err := r.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if _, ok := ran.Load("b"); ok {
		t.Error("default mode must abort before downstream levels")
	}
}

// Context cancellation still aborts between levels in continue mode.
func TestRunner_ContinueModeCtxCancelAborts(t *testing.T) {
	dag := NewDAG()
	dag.AddNode("a", nil)
	dag.AddNode("b", []string{"a"})

	ctx, cancel := context.WithCancel(context.Background())
	var ran sync.Map
	r := NewRunnerBoundedContinue(dag, func(_ context.Context, id string) error {
		ran.Store(id, true)
		cancel() // cancel during level 1
		return errors.New("boom")
	}, 0)
	go func() {
		for range r.Updates() {
		}
	}()
	err := r.Run(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := ran.Load("b"); ok {
		t.Error("ctx cancel must abort before downstream levels even in continue mode")
	}
}
