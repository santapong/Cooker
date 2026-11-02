package store

import (
	"context"

	"github.com/santapong/cooker/internal/model"
)

// RunSourceCheckoutRecorder persists one verified observation without replacing
// concurrent progress, heartbeat or outcome columns. Source-bound workers must
// have this capability before they execute stages.
type RunSourceCheckoutRecorder interface {
	RecordSourceCheckout(context.Context, string, *model.PipelineRunSource) error
}

// ValidateSourceCheckoutWrite requires a live worker claim and complete observed
// source. Implementations still fence that claim under the queue/database lock
// and merge against the already recorded immutable admission lineage.
func ValidateSourceCheckoutWrite(ctx context.Context, runID string, source *model.PipelineRunSource) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	claim, ok := RunClaimFromContext(ctx)
	if !ok || runID == "" || claim.JobID != "run:"+runID || claim.Owner == "" {
		return ErrConflict
	}
	if source == nil || source.Validate() != nil || source.CheckedOutAt == nil || source.CheckedOutAt.IsZero() {
		return model.ErrInvalidRunSource
	}
	return nil
}
