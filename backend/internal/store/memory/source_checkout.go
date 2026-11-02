package memory

import (
	"context"
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

var _ store.RunSourceCheckoutRecorder = (*runs)(nil)

func (s *runs) RecordSourceCheckout(ctx context.Context, id string, source *model.PipelineRunSource) error {
	if err := store.ValidateSourceCheckoutWrite(ctx, id, source); err != nil {
		return err
	}
	return store.GuardRunWrite(ctx, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		r, ok := s.m[id]
		if !ok {
			return fmt.Errorf("run %s: %w", id, store.ErrNotFound)
		}
		if r.Source == nil || r.Status != model.RunStatusPending && r.Status != model.RunStatusRunning {
			return store.ErrConflict
		}
		merged, err := model.MergeRunSource(r.Source, source)
		if err != nil {
			return err
		}
		r.Source = merged.Clone()
		return nil
	})
}
