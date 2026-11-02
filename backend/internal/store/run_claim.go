package store

import "context"

type runClaimKey struct{}

// RunClaim is the worker identity required for domain writes made by a queued
// attempt. Guard is the in-memory queue's atomic check-and-write boundary;
// PostgreSQL stores instead lock the authoritative jobs row in their own tx.
type RunClaim struct {
	JobID string
	Owner string
	Guard func(context.Context, func() error) error
	// PayloadGuard is required only for determinate App rejection writes.
	PayloadGuard func(context.Context, string, string, func() error) error
}

func WithRunClaim(ctx context.Context, jobID, owner string, guard func(context.Context, func() error) error) context.Context {
	return context.WithValue(ctx, runClaimKey{}, RunClaim{JobID: jobID, Owner: owner, Guard: guard})
}

func RunClaimFromContext(ctx context.Context) (RunClaim, bool) {
	c, ok := ctx.Value(runClaimKey{}).(RunClaim)
	return c, ok
}

func GuardRunWrite(ctx context.Context, write func() error) error {
	claim, ok := RunClaimFromContext(ctx)
	if !ok {
		return write()
	}
	if claim.JobID == "" || claim.Owner == "" || claim.Guard == nil {
		return ErrConflict
	}
	if err := claim.Guard(ctx, write); err != nil {
		return err
	}
	return nil
}

// WithRunPayloadClaim preserves the ordinary legacy Guard while adding the
// authoritative immutable-payload boundary for focused atomic outcomes.
func WithRunPayloadClaim(ctx context.Context, guard func(context.Context, string, string, func() error) error) context.Context {
	claim, ok := RunClaimFromContext(ctx)
	if !ok {
		return ctx
	}
	claim.PayloadGuard = guard
	return context.WithValue(ctx, runClaimKey{}, claim)
}
