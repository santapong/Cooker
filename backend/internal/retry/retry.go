// Package retry provides a small backoff loop with caller-supplied
// classification of which errors are worth a retry.
//
// Cooker uses this from the pipeline executor to wrap each builder /
// pusher / deployer call so transient errors (network resets,
// registry 5xx, K8s API blip) don't fail the whole pipeline.
package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// Policy controls the backoff loop. Zero / negative MaxAttempts
// means "run once with no retry"; an empty IsTransient classifier
// retries every error up to MaxAttempts.
type Policy struct {
	// MaxAttempts is the upper bound on total tries (including the
	// first call). One = no retry.
	MaxAttempts int
	// Initial is the wait between attempt 1 and attempt 2; it
	// doubles on each subsequent failure up to Max.
	Initial time.Duration
	// Max is the upper bound on the per-iteration wait.
	Max time.Duration
	// IsTransient is consulted on every error. nil means "retry
	// everything until MaxAttempts is exhausted."
	IsTransient func(error) bool
}

// Default is a sane starting point for stage adapters: three tries,
// 1s → 2s → 4s with up to 50% jitter, all errors retried.
var Default = Policy{
	MaxAttempts: 3,
	Initial:     1 * time.Second,
	Max:         8 * time.Second,
}

// Do runs op until it succeeds, p.MaxAttempts is reached, the error
// is classified non-transient, or ctx is cancelled. The returned
// error is op's last error; ctx errors are returned verbatim.
func Do(ctx context.Context, p Policy, op func(context.Context) error) error {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	if p.Initial <= 0 {
		p.Initial = 500 * time.Millisecond
	}
	if p.Max <= 0 {
		p.Max = 10 * time.Second
	}
	delay := p.Initial
	var lastErr error
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := op(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == p.MaxAttempts {
			break
		}
		if p.IsTransient != nil && !p.IsTransient(err) {
			break
		}
		// Jittered exponential backoff. The jitter is bounded at
		// half the current delay so the worst-case pause is 1.5×
		// of what the policy advertises.
		jittered := delay + time.Duration(rand.Int63n(int64(delay/2)+1))
		t := time.NewTimer(jittered)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
		if delay < p.Max {
			delay *= 2
			if delay > p.Max {
				delay = p.Max
			}
		}
	}
	return lastErr
}

// IsContextErr reports whether err comes from a cancelled or
// deadline-exceeded context. Useful as the negation of "transient".
func IsContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
