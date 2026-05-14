package jobqueue

import (
	"testing"
	"time"
)

func TestExponentialBackoffMonotonicAndCapped(t *testing.T) {
	base := 1 * time.Second
	maxD := 30 * time.Second
	b := ExponentialBackoff{Base: base, Max: maxD}
	// Each NextDelay returns `floor + jitter` where jitter ∈ [0, base/2].
	// The chance of any single sample landing on the no-jitter floor is
	// effectively zero (jitter is computed via crypto/rand mod base/2 in
	// nanoseconds, so the support is ~5×10^8 values for base=1s). Assert
	// the range [floor, floor + base/2] instead — equivalent semantic
	// check, deterministic outcome.
	cases := map[int]time.Duration{
		1: 1 * time.Second,
		2: 2 * time.Second,
		3: 4 * time.Second,
		4: 8 * time.Second,
		5: 16 * time.Second,
		6: 30 * time.Second, // capped
		7: 30 * time.Second, // capped
	}
	maxJitter := base / 2
	for attempt, want := range cases {
		for i := 0; i < 16; i++ {
			got := b.NextDelay(attempt)
			if got < want || got > want+maxJitter {
				t.Errorf("attempt %d sample %d: got=%v want range [%v, %v]",
					attempt, i, got, want, want+maxJitter)
				break
			}
		}
	}
}

func TestExponentialBackoffZeroValuesFallback(t *testing.T) {
	var b ExponentialBackoff // both zero
	// With zero base and zero max the policy must still return a
	// non-negative finite duration, falling back to the documented
	// 1s / 5m defaults. Testing the floor (no jitter).
	d := b.NextDelay(1)
	if d < time.Second || d > 6*time.Minute {
		t.Fatalf("zero-value default fallback off: %v", d)
	}
}

func TestExponentialBackoffHugeAttemptDoesNotOverflow(t *testing.T) {
	b := ExponentialBackoff{Base: 1 * time.Second, Max: 30 * time.Second}
	// A misbehaving caller might pass attempt=10_000. Must clamp
	// without overflowing the bit shift.
	d := b.NextDelay(10000)
	if d > 35*time.Second {
		t.Fatalf("unbounded growth: %v", d)
	}
}

func TestExponentialBackoffAttemptBelowOneClamps(t *testing.T) {
	b := ExponentialBackoff{Base: 2 * time.Second, Max: 30 * time.Second}
	// Treat 0 and negative as attempt=1.
	if b.NextDelay(0) < 2*time.Second {
		t.Fatal("attempt=0 should clamp to 1")
	}
	if b.NextDelay(-1) < 2*time.Second {
		t.Fatal("attempt=-1 should clamp to 1")
	}
}
