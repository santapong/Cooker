package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/store/memory"
)

// A coordinator with cap 1 must reject the second concurrent spawn with
// ErrRunCapacity and accept again once the first run finishes.
func TestRunCoordinator_CapacityRejectAndRelease(t *testing.T) {
	rc := NewRunCoordinator(memory.New())
	rc.sem = semaphore.NewWeighted(1) // force cap=1 regardless of env

	block := make(chan struct{})
	started := make(chan struct{})
	if err := rc.Spawn(context.Background(), "run-1", func(ctx context.Context) error {
		close(started)
		<-block
		return nil
	}); err != nil {
		t.Fatalf("first spawn must be admitted: %v", err)
	}
	<-started

	// Saturated: second spawn is rejected, not blocked.
	err := rc.Spawn(context.Background(), "run-2", func(ctx context.Context) error { return nil })
	if !errors.Is(err, handler.ErrRunCapacity) {
		t.Fatalf("want ErrRunCapacity, got %v", err)
	}

	// Free the slot; a new spawn must be admitted again.
	close(block)
	deadline := time.After(2 * time.Second)
	for {
		if err := rc.Spawn(context.Background(), "run-3", func(ctx context.Context) error { return nil }); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("slot never released after run completion")
		case <-time.After(10 * time.Millisecond):
		}
	}
	rc.Wait(context.Background())
}

// Cap 0 (unlimited) never rejects.
func TestRunCoordinator_UnlimitedWhenNoSem(t *testing.T) {
	rc := NewRunCoordinator(memory.New())
	rc.sem = nil
	for i := 0; i < 20; i++ {
		if err := rc.Spawn(context.Background(), "r", func(ctx context.Context) error { return nil }); err != nil {
			t.Fatalf("unlimited coordinator rejected: %v", err)
		}
	}
	rc.Wait(context.Background())
}
