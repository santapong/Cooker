package jobqueue

import (
	crand "crypto/rand"
	"encoding/binary"
	"time"
)

// BackoffPolicy decides how long to wait before re-attempting a
// failed job. Implementations are stateless and safe to share across
// goroutines.
type BackoffPolicy interface {
	NextDelay(attempt int) time.Duration
}

// ExponentialBackoff: base * 2^(attempt-1) capped at max, plus a
// random jitter in [0, base/2] so a thundering herd of synchronised
// failures (e.g. a flaky downstream) doesn't all retry at the same
// instant. attempt is 1-based: the first retry uses attempt=1.
type ExponentialBackoff struct {
	Base time.Duration // delay after the first failure
	Max  time.Duration // ceiling once doubling overflows it
}

// DefaultBackoff is the policy used when a Pool is constructed
// without an explicit one. 5s start, 5m ceiling matches the order
// of magnitude of typical CI/CD transient failures (network blips,
// short rate-limit windows).
var DefaultBackoff BackoffPolicy = ExponentialBackoff{
	Base: 5 * time.Second,
	Max:  5 * time.Minute,
}

// NextDelay implements BackoffPolicy.
func (e ExponentialBackoff) NextDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := e.Base
	if base <= 0 {
		base = 1 * time.Second
	}
	maxD := e.Max
	if maxD <= 0 {
		maxD = 5 * time.Minute
	}
	// shift caps at attempt-1 so attempt=1 → base, attempt=2 → 2*base
	shift := attempt - 1
	if shift > 30 {
		shift = 30 // 2^30 ≈ 1B, more than enough to saturate Max
	}
	d := base * (1 << shift)
	if d <= 0 || d > maxD {
		d = maxD
	}
	d += jitter(base / 2)
	return d
}

// jitter returns a non-negative duration in [0, d]. Uses crypto/rand
// to avoid the locked global math/rand source; on the very unlikely
// event of read failure, returns 0 (deterministic fallback).
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return 0
	}
	n := int64(binary.BigEndian.Uint64(b[:]) & ((1 << 62) - 1))
	return time.Duration(n % int64(d+1))
}
