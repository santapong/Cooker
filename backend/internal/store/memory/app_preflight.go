package memory

import (
	"context"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"time"
)

func (s *appDeploys) RecordAppPreflightFailure(ctx context.Context, f store.AppPreflightFailure) error {
	claim, ok := store.RunClaimFromContext(ctx)
	if !ok || claim.JobID != "app-run:"+f.RunID || claim.Owner == "" || claim.PayloadGuard == nil || f.Validate() != nil || s.apps == nil || s.runs == nil {
		return store.ErrConflict
	}
	covered, err := store.DecodeAppPreflightFailure(f.AcceptedPayload)
	if err != nil || !store.SameAppPreflightAcceptance(f, covered) {
		return store.ErrConflict
	}
	return claim.PayloadGuard(ctx, "app-deploy", f.PayloadDigest, func() error {
		s.apps.mu.RLock()
		defer s.apps.mu.RUnlock()
		s.runs.mu.Lock()
		defer s.runs.mu.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.apps.m[f.AppID] == nil {
			return store.ErrNotFound
		}
		run := s.runs.m[f.RunID]
		if err := store.ValidateAppPreflightRun(f, run); err != nil {
			return err
		}
		var existing *model.AppDeploy
		for _, d := range s.m {
			if d.RunID == f.RunID || d.ID == store.AppPreflightHistoryID(f.RunID) {
				if existing != nil || !store.AppPreflightMarkerValid(f, run, d) {
					return store.ErrConflict
				}
				existing = d
			}
		}
		if existing != nil {
			return nil
		}
		cp := run.Clone()
		if cp.Status == model.RunStatusPending {
			now := time.Now().UTC().Truncate(time.Microsecond)
			cp.Status, cp.Error, cp.FinishedAt = model.RunStatusFailed, store.AppPreflightSummary(f.Reason), &now
		}
		d := &model.AppDeploy{ID: store.AppPreflightHistoryID(f.RunID), AppID: f.AppID, RunID: f.RunID, PipelineID: f.PipelineID, Status: model.RunStatusFailed, Kind: model.AppDeployKindRejected, CreatedAt: *cp.FinishedAt}
		if !store.AppPreflightMarkerValid(f, cp, d) {
			return store.ErrConflict
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		s.runs.m[f.RunID] = cp
		s.m[d.ID] = d
		return nil
	})
}
func (s *appDeploys) HasAppPreflightFailure(ctx context.Context, f store.AppPreflightFailure) (bool, error) {
	if f.Validate() != nil || s.runs == nil {
		return false, store.ErrConflict
	}
	s.runs.mu.RLock()
	defer s.runs.mu.RUnlock()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	run := s.runs.m[f.RunID]
	if reason, recorded := store.AppPreflightReasonFromRun(run); recorded {
		f.Reason = reason
	}
	if store.ValidateAppPreflightLineage(f, run) != nil {
		return false, store.ErrConflict
	}
	count, typed := 0, false
	valid := false
	for _, d := range s.m {
		if d.RunID == f.RunID || d.ID == store.AppPreflightHistoryID(f.RunID) {
			if d.AppID != f.AppID || d.RunID != f.RunID || d.PipelineID != "" && d.PipelineID != f.PipelineID {
				return false, store.ErrConflict
			}
			count++
			typed = typed || d.Kind == model.AppDeployKindRejected
			valid = store.AppPreflightMarkerValid(f, run, d)
		}
	}
	if typed && (count != 1 || !valid) {
		return false, store.ErrConflict
	}
	return count == 1 && valid, nil
}

func (s *appDeploys) CheckAppLegacyRecovery(ctx context.Context, runID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, d := range s.m {
		if (d.RunID == runID || d.ID == store.AppPreflightHistoryID(runID)) && d.Kind == model.AppDeployKindRejected {
			return store.ErrConflict
		}
	}
	return nil
}
