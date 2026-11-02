package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// Project only public identity and lifecycle fields. Neither spec/event JSON,
// job payloads, run variables/errors/logs nor credentials leave PostgreSQL.
const triggerHistoryCTE = `WITH expanded AS (
 SELECT d.*, x.decision, x.ordinality,x.journaled
 FROM webhook_deliveries d
 CROSS JOIN LATERAL (
  SELECT jsonb_build_object('ruleId',j.rule_id,'ruleVersion',j.rule_version,'appId',j.app_id,'environmentId',j.environment_id,'targetKind',j.target_kind,'targetId',j.target_id,'intentId',j.intent_id,'status',j.outcome,'reasonCode',j.reason_code),j.decision_index::bigint,TRUE
  FROM trigger_delivery_decisions j WHERE j.delivery_id=d.id
  UNION ALL
  SELECT decision, ordinality,FALSE FROM jsonb_array_elements(
   CASE WHEN jsonb_typeof(d.decisions)='array' THEN d.decisions ELSE '[]'::jsonb END
  ) WITH ORDINALITY AS x(decision,ordinality) WHERE NOT EXISTS(SELECT 1 FROM trigger_delivery_decisions j WHERE j.delivery_id=d.id)
  UNION ALL
  SELECT jsonb_build_object('intentId',i.id,'ruleId',i.rule_id), row_number() OVER(ORDER BY i.id),FALSE
  FROM trigger_intents i WHERE i.delivery_id=d.id AND d.archived
   AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(d.decisions)='array' THEN d.decisions ELSE '[]'::jsonb END)) AND NOT EXISTS(SELECT 1 FROM trigger_delivery_decisions j WHERE j.delivery_id=d.id)
  UNION ALL
  SELECT '{}'::jsonb,1::bigint,FALSE WHERE
   NOT EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(d.decisions)='array' THEN d.decisions ELSE '[]'::jsonb END))
   AND NOT EXISTS(SELECT 1 FROM trigger_intents i WHERE i.delivery_id=d.id AND d.archived) AND NOT EXISTS(SELECT 1 FROM trigger_delivery_decisions j WHERE j.delivery_id=d.id)
 ) AS x(decision,ordinality,journaled)
), observed AS (
 SELECT d.id AS delivery_id,d.delivery_id AS provider_delivery_id,d.provider,d.connection_id,
 COALESCE(d.event->'source'->>'repository','') AS repository,
 COALESCE(d.event->>'fullRef','') AS full_ref,COALESCE(d.event->>'commitSha','') AS commit_sha,
 COALESCE(d.event->>'deleted','false')='true' AS deleted,
 COALESCE(d.decision->>'ruleId','') AS rule_id,
 COALESCE(d.decision->>'ruleVersion',i.spec->'rule'->>'version','0') AS rule_version,
 CASE WHEN d.journaled THEN COALESCE(d.decision->>'appId','') ELSE COALESCE(i.spec->'rule'->>'appId',r.body->>'appId','') END AS app_id,
 CASE WHEN d.journaled THEN 'decision' WHEN i.id IS NOT NULL THEN 'intent' WHEN r.id IS NOT NULL THEN 'current_rule' ELSE '' END AS target_source,
 CASE WHEN d.journaled THEN 'retained_decision' ELSE 'legacy' END AS metadata_coverage,
 CASE WHEN d.journaled THEN COALESCE(d.decision->>'environmentId','') ELSE '' END AS environment_id,CASE WHEN d.journaled THEN COALESCE(d.decision->>'targetKind','') ELSE '' END AS target_kind,CASE WHEN d.journaled THEN COALESCE(d.decision->>'targetId','') ELSE '' END AS target_id,
 COALESCE(NULLIF(d.decision->>'intentId',''),i.id,'') AS intent_id,COALESCE(i.status,'') AS intent_status,
 COALESCE(i.receipt->>'jobId','') AS job_id,COALESCE(j.status,'') AS job_status,
 COALESCE(i.receipt->>'runId','') AS run_id,COALESCE(pr.status,'') AS run_status,
 COALESCE(i.receipt->>'pipelineId','') AS pipeline_id,
 COALESCE(d.decision->>'status','') AS decision_status,
 COALESCE(CASE WHEN d.decision->>'status'='duplicate' THEN d.decision->>'reason' ELSE COALESCE(i.reason,d.decision->>'reason','') END,'') AS reason,
 COALESCE(d.decision->>'reasonCode','') AS reason_code,
 d.created_at,GREATEST(COALESCE(i.updated_at,d.created_at),COALESCE(j.updated_at,d.created_at),COALESCE(pr.finished_at,pr.started_at,d.created_at)) AS updated_at,d.archived,d.ordinality
 FROM expanded d LEFT JOIN trigger_intents i ON i.id=d.decision->>'intentId'
 LEFT JOIN trigger_rules r ON r.id=d.decision->>'ruleId'
 LEFT JOIN jobs j ON j.id=i.receipt->>'jobId'
 LEFT JOIN pipeline_runs pr ON pr.id=i.receipt->>'runId' AND pr.pipeline_id=i.receipt->>'pipelineId'
), projected AS (
 SELECT *,CASE
  WHEN decision_status IN('duplicate','ignored','rejected') THEN decision_status
  WHEN intent_status='queued' AND (job_status='failed' OR run_status='failed') THEN 'failed'
  WHEN intent_status='queued' AND job_status='needs_attention' THEN 'review_required'
  WHEN COALESCE(NULLIF(intent_status,''),decision_status) IN('pending','queued','accepted') THEN 'accepted'
  WHEN COALESCE(NULLIF(intent_status,''),decision_status) IN('awaiting_review','review_required') THEN 'review_required'
  WHEN COALESCE(NULLIF(intent_status,''),decision_status) IN('superseded','stale') THEN 'stale'
  WHEN COALESCE(NULLIF(intent_status,''),decision_status) IN('failed_preparation','failed') THEN 'failed'
  WHEN COALESCE(NULLIF(intent_status,''),decision_status)='unknown' THEN 'unknown'
  WHEN archived THEN 'archived' ELSE 'unknown' END AS status
 FROM observed
), filtered AS (
 SELECT * FROM projected WHERE
 ($1='' OR app_id=$1) AND ($2='' OR provider=$2) AND ($3='' OR connection_id=$3)
 AND ($4='' OR status=$4) AND ($5='' OR full_ref=$5) AND ($6='' OR delivery_id=$6)
 AND ($7='' OR strpos(lower(concat_ws(' ',delivery_id,provider_delivery_id,provider,repository,full_ref,commit_sha,rule_id,app_id,target_kind,target_id,intent_id,job_id,run_id)),lower($7))>0)
 AND ($8='' OR target_kind=$8) AND ($9='' OR target_id=$9)
) `

func (t *TriggerStore) ListTriggerHistory(ctx context.Context, query store.TriggerHistoryQuery) (*model.TriggerHistoryPage, error) {
	q, err := query.Normalize()
	if err != nil {
		return nil, err
	}
	page := store.NewTriggerHistoryPage(q)
	// Count and page share a snapshot; transitions cannot change totals between
	// these reads. PostgreSQL performs all filters before LIMIT/OFFSET.
	tx, err := t.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var cutover time.Time
	if err = tx.QueryRowContext(ctx, `SELECT started_at FROM trigger_audit_checkpoint WHERE id='v1'`).Scan(&cutover); err != nil {
		return nil, err
	}
	page.JournalStartedAt = &cutover
	args := []any{q.AppID, q.Provider, q.ConnectionID, q.Status, q.Ref, q.DeliveryID, q.Search, q.TargetKind, q.TargetID}
	if err = tx.QueryRowContext(ctx, triggerHistoryCTE+`SELECT COUNT(*) FROM filtered`, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := tx.QueryContext(ctx, triggerHistoryCTE+`SELECT delivery_id,provider_delivery_id,provider,connection_id,repository,full_ref,commit_sha,deleted,rule_id,rule_version,app_id,target_source,metadata_coverage,environment_id,target_kind,target_id,intent_id,intent_status,job_id,job_status,run_id,run_status,pipeline_id,decision_status,reason,reason_code,created_at,updated_at,archived,ordinality FROM filtered ORDER BY created_at DESC,delivery_id,ordinality LIMIT $10 OFFSET $11`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row model.TriggerHistoryRow
		if err = rows.Scan(&row.DeliveryID, &row.ProviderDeliveryID, &row.Provider, &row.ConnectionID, &row.Repository, &row.FullRef, &row.CommitSHA, &row.Deleted, &row.RuleID, &row.RuleVersion, &row.AppID, &row.TargetSource, &row.MetadataCoverage, &row.EnvironmentID, &row.TargetKind, &row.TargetID, &row.IntentID, &row.IntentStatus, &row.JobID, &row.JobStatus, &row.RunID, &row.RunStatus, &row.PipelineID, &row.StoredStatus, &row.StoredReason, &row.StoredReasonCode, &row.CreatedAt, &row.UpdatedAt, &row.Archived, &row.DecisionIndex); err != nil {
			return nil, err
		}
		page.Items = append(page.Items, store.PresentTriggerHistory(row))
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return page, nil
}

var _ store.TriggerHistoryStore = (*TriggerStore)(nil)
