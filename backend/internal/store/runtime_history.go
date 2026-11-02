package store

import (
	"context"
	"fmt"

	"github.com/santapong/cooker/internal/model"
)

// AppDeploymentObservationReader keeps a conclusively rejected, untouched
// attempt separate from the latest attempt that could have affected runtime.
// Failed and uncertain runtime attempts remain candidates; this is not health
// evidence or a selection of the last successful release.
type AppDeploymentObservationReader interface {
	LatestRuntimeAttemptByApps(context.Context, []string) (map[string]*model.AppDeploy, error)
}

// LatestRuntimeAttempts uses the bulk reader in production. The bounded
// compatibility path for older adapters never skips ordinary failures or
// unknown kinds and returns unavailable when its window contains only rejections.
func LatestRuntimeAttempts(ctx context.Context, history AppDeployStore, ids []string) (map[string]*model.AppDeploy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if history == nil {
		return nil, fmt.Errorf("deployment history is unavailable")
	}
	if reader, ok := history.(AppDeploymentObservationReader); ok {
		return reader.LatestRuntimeAttemptByApps(ctx, ids)
	}
	out := map[string]*model.AppDeploy{}
	for _, id := range ids {
		rows, err := history.ListByApp(ctx, id, 100)
		if err != nil {
			return nil, err
		}
		for _, d := range rows {
			if d != nil && d.Kind != model.AppDeployKindRejected {
				cp := *d
				out[id] = &cp
				break
			}
		}
		if out[id] == nil && len(rows) == 100 {
			return nil, fmt.Errorf("runtime deployment is outside the history window")
		}
	}
	return out, nil
}
