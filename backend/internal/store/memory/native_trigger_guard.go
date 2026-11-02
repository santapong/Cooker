package memory

import (
	"context"
	"fmt"
)

// The fixture/development store serializes native preparation conservatively.
// Production PostgreSQL scopes the lock by intent; this is not a durable queue.
func (t *TriggerStore) WithNativeTriggerAdmission(ctx context.Context, id string, work func(context.Context) error) error {
	if id == "" || work == nil {
		return fmt.Errorf("native admission identity is required")
	}
	select {
	case t.nativeAdmission <- struct{}{}:
		defer func() { <-t.nativeAdmission }()
		return work(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}
