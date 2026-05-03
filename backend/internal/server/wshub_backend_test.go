package server

import (
	"context"
	"testing"
	"time"
)

func TestNextBackoff_DoublesAndCaps(t *testing.T) {
	max := 10 * time.Second
	cases := []struct {
		in, want time.Duration
	}{
		{500 * time.Millisecond, 1 * time.Second},
		{1 * time.Second, 2 * time.Second},
		{4 * time.Second, 8 * time.Second},
		{8 * time.Second, max}, // would double to 16s, capped at 10s
		{max, max},             // already at max stays at max
	}
	for _, c := range cases {
		if got := nextBackoff(c.in, max); got != c.want {
			t.Errorf("nextBackoff(%s, %s) = %s; want %s", c.in, max, got, c.want)
		}
	}
}

func TestSleepJitter_ReturnsTrueOnTimer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	if !sleepJitter(ctx, 10*time.Millisecond) {
		t.Fatal("expected sleepJitter to return true when timer fires")
	}
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("returned in %s; expected >= 10ms", elapsed)
	}
	// Upper bound: base + jitter (50%) + scheduler slack
	if elapsed > 200*time.Millisecond {
		t.Errorf("returned in %s; expected < 200ms", elapsed)
	}
}

func TestSleepJitter_ReturnsFalseOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if sleepJitter(ctx, 10*time.Second) {
		t.Fatal("expected sleepJitter to return false when ctx is cancelled")
	}
	elapsed := time.Since(start)
	// Should bail out well before the 10-second timer.
	if elapsed > 500*time.Millisecond {
		t.Errorf("did not bail out promptly on ctx cancel: %s", elapsed)
	}
}
