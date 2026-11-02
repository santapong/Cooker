package postgres

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

var _ store.CatalogPipelineReader = (*PipelineStore)(nil)
var _ store.CatalogAppDeployReader = (*AppDeployStore)(nil)
var _ store.AppDeploymentObservationReader = (*AppDeployStore)(nil)

func (s *PipelineStore) ListCatalog(ctx context.Context) ([]store.CatalogPipelineSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,jsonb_array_length(CASE WHEN jsonb_typeof(p.stages)='array' THEN p.stages ELSE '[]'::jsonb END),p.updated_at,r.id,r.status,r.created_at,r.started_at,r.finished_at FROM pipelines p LEFT JOIN LATERAL (SELECT id,status,created_at,started_at,finished_at FROM pipeline_runs WHERE pipeline_id=p.id ORDER BY created_at DESC,id DESC LIMIT 1) r ON true ORDER BY p.updated_at DESC,p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.CatalogPipelineSummary{}
	for rows.Next() {
		var item store.CatalogPipelineSummary
		var runID, status sql.NullString
		var created, started, finished sql.NullTime
		if err := rows.Scan(&item.ID, &item.Name, &item.StageCount, &item.UpdatedAt, &runID, &status, &created, &started, &finished); err != nil {
			return nil, err
		}
		if runID.Valid {
			r := &model.PipelineRun{ID: runID.String, PipelineID: item.ID, Status: model.RunStatus(status.String), CreatedAt: created.Time}
			if started.Valid {
				r.StartedAt = &started.Time
			}
			if finished.Valid {
				r.FinishedAt = &finished.Time
			}
			item.LatestRun = r
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *AppDeployStore) LatestByApps(ctx context.Context, ids []string) (map[string]*model.AppDeploy, error) {
	return s.latestByApps(ctx, ids, false)
}

func (s *AppDeployStore) LatestRuntimeAttemptByApps(ctx context.Context, ids []string) (map[string]*model.AppDeploy, error) {
	return s.latestByApps(ctx, ids, true)
}

func (s *AppDeployStore) latestByApps(ctx context.Context, ids []string, runtimeOnly bool) (map[string]*model.AppDeploy, error) {
	out := map[string]*model.AppDeploy{}
	if len(ids) == 0 {
		return out, nil
	}
	// The existing (app_id,created_at DESC) index supports one latest row for
	// each requested App, without loading its complete deployment history.
	predicate := ""
	args := []any{pq.Array(ids)}
	if runtimeOnly {
		predicate = " AND kind <> $2"
		args = append(args, string(model.AppDeployKindRejected))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.app_id,d.run_id,d.pipeline_id,d.image_ref,d.digest,d.status,d.kind,d.created_at FROM (SELECT DISTINCT unnest($1::text[]) AS app_id) requested JOIN LATERAL (SELECT id,app_id,run_id,pipeline_id,image_ref,digest,status,kind,created_at FROM app_deploys WHERE app_id=requested.app_id`+predicate+` ORDER BY created_at DESC,id DESC LIMIT 1) d ON true`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanAppDeploy(rows)
		if err != nil {
			return nil, err
		}
		out[d.AppID] = d
	}
	return out, rows.Err()
}
