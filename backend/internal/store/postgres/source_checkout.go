package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

var _ store.RunSourceCheckoutRecorder = (*RunStore)(nil)

func (s *RunStore) RecordSourceCheckout(ctx context.Context, id string, source *model.PipelineRunSource) error {
	if err := store.ValidateSourceCheckoutWrite(ctx, id, source); err != nil {
		return err
	}
	// withRunClaim locks the authoritative job before this domain row, matching
	// cancellation/recovery. The source-only UPDATE preserves concurrent fields.
	return withRunClaim(ctx, s.db, id, func(writer claimWriter) error {
		var data []byte
		var status model.RunStatus
		if err := writer.QueryRowContext(ctx, `SELECT source,status FROM pipeline_runs WHERE id=$1 FOR UPDATE`, id).Scan(&data, &status); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("run %s: %w", id, store.ErrNotFound)
		} else if err != nil {
			return err
		}
		var previous *model.PipelineRunSource
		if status != model.RunStatusPending && status != model.RunStatusRunning || len(data) == 0 || json.Unmarshal(data, &previous) != nil || previous == nil {
			return store.ErrConflict
		}
		merged, err := model.MergeRunSource(previous, source)
		if err != nil {
			return err
		}
		encoded, err := marshalRunSource(merged)
		if err != nil {
			return err
		}
		encodedBytes, ok := encoded.([]byte)
		if !ok {
			return store.ErrConflict
		}
		var recorded []byte
		if err := writer.QueryRowContext(ctx, `UPDATE pipeline_runs SET source=$2 WHERE id=$1 RETURNING source`, id, encoded).Scan(&recorded); errors.Is(err, sql.ErrNoRows) {
			return store.ErrConflict
		} else if err != nil {
			return err
		}
		// A skipped/rewritten write is not an acknowledgement. Compare the
		// actual JSONB value, including unexpected fields, before committing.
		var expected, actual any
		if json.Unmarshal(encodedBytes, &expected) != nil || json.Unmarshal(recorded, &actual) != nil || !reflect.DeepEqual(expected, actual) {
			return store.ErrConflict
		}
		return nil
	})
}
