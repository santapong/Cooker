package memory

import (
	"context"
	"sort"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// Embedding preserves existing pipeline operations; the aggregate constructor
// provides the same runs instance used by Store.Runs for a single bulk read.
type catalogPipelines struct {
	*pipelines
	runs *runs
}

var _ store.CatalogPipelineReader = (*catalogPipelines)(nil)
var _ store.CatalogAppDeployReader = (*appDeploys)(nil)

func (s *catalogPipelines) ListCatalog(ctx context.Context) ([]store.CatalogPipelineSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.runs.mu.RLock()
	defer s.runs.mu.RUnlock()
	latest := map[string]*model.PipelineRun{}
	for _, r := range s.runs.m {
		old := latest[r.PipelineID]
		if old == nil || r.CreatedAt.After(old.CreatedAt) || (r.CreatedAt.Equal(old.CreatedAt) && r.ID > old.ID) {
			latest[r.PipelineID] = r
		}
	}
	out := make([]store.CatalogPipelineSummary, 0, len(s.m))
	for _, p := range s.m {
		item := store.CatalogPipelineSummary{ID: p.ID, Name: p.Name, StageCount: len(p.Stages), UpdatedAt: p.UpdatedAt}
		if r := latest[p.ID]; r != nil {
			item.LatestRun = (&model.PipelineRun{ID: r.ID, PipelineID: r.PipelineID, Status: r.Status, CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt}).Clone()
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}
func (s *appDeploys) LatestByApps(ctx context.Context, ids []string) (map[string]*model.AppDeploy, error) {
	return s.latestByApps(ctx, ids, false)
}

func (s *appDeploys) LatestRuntimeAttemptByApps(ctx context.Context, ids []string) (map[string]*model.AppDeploy, error) {
	return s.latestByApps(ctx, ids, true)
}

func (s *appDeploys) latestByApps(ctx context.Context, ids []string, runtimeOnly bool) (map[string]*model.AppDeploy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := map[string]*model.AppDeploy{}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	if len(wanted) == 0 {
		return out, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.m {
		if !wanted[d.AppID] || runtimeOnly && d.Kind == model.AppDeployKindRejected {
			continue
		}
		old := out[d.AppID]
		if old == nil || d.CreatedAt.After(old.CreatedAt) || (d.CreatedAt.Equal(old.CreatedAt) && d.ID > old.ID) {
			cp := *d
			out[d.AppID] = &cp
		}
	}
	return out, nil
}
