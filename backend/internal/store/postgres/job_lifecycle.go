package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type recoveryJobPayload struct {
	RunID      string                      `json:"runId"`
	PipelineID string                      `json:"pipelineId"`
	Pipeline   *model.Pipeline             `json:"pipeline"`
	Source     *model.PipelineSourcePlan   `json:"source"`
	Trigger    *model.PipelineTriggerProof `json:"trigger"`
	App        *model.App                  `json:"app"`
	Actor      *model.PipelineRun          `json:"actor"`
	Run        *model.PipelineRun          `json:"run"`
	CreatedAt  time.Time                   `json:"createdAt"`
}

// SyncJobRun executes inside the same transaction that revokes or resolves a
// claim. A failed domain update rolls back the job transition, so the next
// recovery sweep can retry it. The owning-job lock also fences late workers.
func SyncJobRun(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
	if j.Kind != "pipeline-run" && j.Kind != "app-deploy" {
		return nil
	}
	var payload recoveryJobPayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil || payload.RunID == "" {
		return fmt.Errorf("cannot recover job with invalid run identity")
	}
	var source *model.PipelineRunSource
	if j.Kind == "pipeline-run" {
		var err error
		source, err = recoveredPipelineSource(&payload)
		if err != nil {
			return err
		}
	}
	if j.Kind == "pipeline-run" && payload.Run != nil {
		run := payload.Run
		if run.ID != payload.RunID || run.PipelineID != payload.PipelineID || run.Status != model.RunStatusPending || run.CreatedAt.IsZero() || run.StartedAt != nil || run.FinishedAt != nil || payload.Pipeline != nil && (payload.Pipeline.ID != payload.PipelineID || payload.Pipeline.Version != run.PipelineVersion) {
			return fmt.Errorf("pipeline deployment plan has an invalid pending run")
		}
		sourceJSON, err := marshalRunSource(source)
		if err != nil {
			return fmt.Errorf("pipeline deployment plan has invalid source metadata")
		}
		stages, err := json.Marshal(run.StageRuns)
		if err != nil {
			return err
		}
		envs, err := json.Marshal(run.EnvironmentStatuses)
		if err != nil {
			return err
		}
		variables, err := json.Marshal(run.Variables)
		if err != nil {
			return err
		}
		groups, err := json.Marshal(nonNilStrings(run.StartedByGroups))
		if err != nil {
			return err
		}
		// Recreate only the view, never a user-deleted pipeline definition.
		if _, err := tx.ExecContext(ctx, `INSERT INTO pipeline_runs (id,pipeline_id,status,stage_runs,env_statuses,variables,created_at,started_by_user_sub,started_by_email,started_by_groups,started_by_token_hash,pipeline_version,source)
		SELECT $1,$2,'pending',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12 WHERE EXISTS (SELECT 1 FROM pipelines WHERE id=$2)
		ON CONFLICT (id) DO NOTHING`, run.ID, run.PipelineID, stages, envs, variables, run.CreatedAt, run.StartedByUserSub, run.StartedByEmail, groups, run.StartedByTokenHash, run.PipelineVersion, sourceJSON); err != nil {
			return err
		}
	}
	if j.Kind == "pipeline-run" {
		if err := mergeRecoveredRunSource(ctx, tx, &payload, source); err != nil {
			return err
		}
	}
	// Accepted app work can crash before materializing its run view. Recreate
	// that view from the durable plan before applying the verified lifecycle.
	if j.Kind == "app-deploy" {
		if payload.App == nil {
			return fmt.Errorf("app deployment plan is missing app identity")
		}
		id := "app-" + payload.App.ID + "-" + payload.RunID
		if _, err := tx.ExecContext(ctx, `INSERT INTO pipelines (id,name,stages,edges,variables,created_at,updated_at)
		VALUES ($1,$2,'[]','[]','{}',$3,$3) ON CONFLICT (id) DO NOTHING`, id, "Deploy "+payload.App.Name, payload.CreatedAt); err != nil {
			return err
		}
		actor := payload.Actor
		if actor == nil {
			actor = &model.PipelineRun{}
		}
		groups, _ := json.Marshal(nonNilStrings(actor.StartedByGroups))
		if _, err := tx.ExecContext(ctx, `INSERT INTO pipeline_runs (id,pipeline_id,status,stage_runs,env_statuses,variables,created_at,started_by_user_sub,started_by_email,started_by_groups,started_by_token_hash)
		VALUES ($1,$2,'pending','[]','[]','{}',$3,$4,$5,$6,$7) ON CONFLICT (id) DO NOTHING`, payload.RunID, id, payload.CreatedAt, actor.StartedByUserSub, actor.StartedByEmail, groups, actor.StartedByTokenHash); err != nil {
			return err
		}
	}
	// The queue transition already holds the authoritative job lock. A complete
	// typed preflight rejection is determinate business evidence even when queue
	// completion was lost. Missing/partial markers keep ordinary recovery rules.
	if j.Kind == "app-deploy" {
		failure, decodeErr := store.DecodeAppPreflightFailure(j.Payload)
		if decodeErr == nil {
			var appExists int
			appErr := tx.QueryRowContext(ctx, `SELECT 1 FROM apps WHERE id=$1 FOR SHARE`, failure.AppID).Scan(&appExists)
			if appErr != nil && !errors.Is(appErr, sql.ErrNoRows) {
				return appErr
			}
			run, readErr := lockedPreflightRun(ctx, tx, failure.RunID)
			if readErr != nil {
				return readErr
			}
			if reason, recorded := store.AppPreflightReasonFromRun(run); recorded {
				failure.Reason = reason
			}
			if store.ValidateAppPreflightLineage(failure, run) != nil {
				return fmt.Errorf("cannot recover deployment with contradictory accepted identity")
			}
			rows, readErr := lockedPreflightHistory(ctx, tx, failure)
			if readErr != nil {
				return readErr
			}
			for _, row := range rows {
				if row.AppID != failure.AppID || row.RunID != failure.RunID || row.PipelineID != "" && row.PipelineID != failure.PipelineID {
					return fmt.Errorf("cannot recover deployment with contradictory history identity")
				}
			}
			var hasLogs bool
			if readErr = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM stage_log_entries WHERE run_id=$1)`, failure.RunID).Scan(&hasLogs); readErr != nil {
				return readErr
			}
			if !hasLogs && len(rows) == 1 && store.AppPreflightMarkerValid(failure, run, rows[0]) {
				return nil
			}
			for _, row := range rows {
				if row.Kind == model.AppDeployKindRejected {
					return fmt.Errorf("cannot recover deployment with invalid rejection evidence")
				}
			}
		} else {
			if !store.IsLegacyAppDeploymentPayload(j.Payload) {
				return fmt.Errorf("cannot recover deployment with invalid accepted envelope")
			}
			// A missing version cannot strip a previously committed rejection.
			// Retain job→App→run→history lock order for this legacy-only probe.
			var exists int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM apps WHERE id=$1 FOR SHARE`, payload.App.ID).Scan(&exists)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if _, err = lockedPreflightRun(ctx, tx, payload.RunID); err != nil {
				return err
			}
			legacy := store.AppPreflightFailure{AppID: payload.App.ID, RunID: payload.RunID, PipelineID: "app-" + payload.App.ID + "-" + payload.RunID}
			rows, err := lockedPreflightHistory(ctx, tx, legacy)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if row.Kind == model.AppDeployKindRejected {
					return fmt.Errorf("cannot recover deployment with unbound rejection evidence")
				}
			}
		}
	}
	status := model.RunStatusNeedsAttention
	var finished *time.Time
	switch j.Status {
	case jobqueue.StatusSucceeded:
		status = model.RunStatusSuccess
	case jobqueue.StatusFailed:
		status = model.RunStatusFailed
	case jobqueue.StatusCancelled:
		status = model.RunStatusCancelled
	case jobqueue.StatusNeedsAttention:
	default:
		return nil
	}
	if j.Status != jobqueue.StatusNeedsAttention {
		now := time.Now().UTC()
		finished = &now
	}
	res, err := tx.ExecContext(ctx, `UPDATE pipeline_runs SET status=$2,error=$3,finished_at=$4 WHERE id=$1`, payload.RunID, status, j.LastError, finished)
	if err != nil {
		return err
	}
	if j.Kind == "app-deploy" {
		if _, err := tx.ExecContext(ctx, `UPDATE app_deploys SET status=$2 WHERE run_id=$1`, payload.RunID, status); err != nil {
			return err
		}
		// A worker may disappear after a runtime mutation but before its terminal
		// history write. Preserve the attempt as uncertain instead of hiding it.
		// Only identity/status are recoverable here: never invent an image digest
		// or infer successful rollout. Existing artifact evidence stays intact.
		historyID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("cooker-recovery-history:"+payload.RunID)).String()
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_deploys (id,app_id,run_id,pipeline_id,status,kind)
		SELECT $1,$2,r.id,r.pipeline_id,$4,'deploy' FROM pipeline_runs r
		WHERE r.id=$3 AND EXISTS (SELECT 1 FROM apps WHERE id=$2)
		AND NOT EXISTS (SELECT 1 FROM app_deploys WHERE run_id=$3)
		ON CONFLICT (id) DO NOTHING`, historyID, payload.App.ID, payload.RunID, status); err != nil {
			return err
		}
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		if j.Kind == "pipeline-run" && payload.Run != nil {
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pipelines WHERE id=$1)`, payload.PipelineID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				// The deletion cascaded the view. Keep the queue's explicit
				// cancellation/review outcome without resurrecting deleted data.
				return nil
			}
		}
		return fmt.Errorf("cannot transition missing deployment run")
	}
	return nil
}

// recoveredPipelineSource validates captured metadata only. It does not grant
// execution authority, inspect provider credentials, or rediscover current refs.
// The worker independently validates admission/authority and verifies checkout.
func recoveredPipelineSource(payload *recoveryJobPayload) (*model.PipelineRunSource, error) {
	invalid := func() (*model.PipelineRunSource, error) {
		return nil, fmt.Errorf("pipeline deployment plan has invalid source metadata")
	}
	p, run, plan := payload.Pipeline, payload.Run, payload.Source
	bound := plan != nil || payload.Trigger != nil || p != nil && p.Source != nil || run != nil && run.Source != nil
	if !bound {
		return nil, nil
	}
	if p == nil || p.Source == nil || run == nil || plan == nil || p.ID != payload.PipelineID || p.Version < 1 || run.ID != payload.RunID || run.PipelineID != p.ID || run.PipelineVersion != p.Version || plan.Version != 1 || plan.PipelineID != p.ID || plan.PipelineVersion != p.Version || plan.ConnectionVersion < 1 || plan.Source.ConnectionID != p.Source.ConnectionID || plan.Source.Repository != p.Source.Repository || plan.EnvironmentVersions == nil || len(plan.EnvironmentVersions) > 100 || plan.ReviewPolicy != "manual" && plan.ReviewPolicy != "review" && plan.ReviewPolicy != "automatic" {
		return invalid()
	}
	for _, stage := range run.StageRuns {
		if stage.StartedAt != nil || stage.FinishedAt != nil || stage.Status != model.RunStatusPending {
			return invalid()
		}
	}
	receipt, err := model.RunSourceFromAcceptedPlan(plan)
	if err != nil {
		return invalid()
	}
	// The authored default ref can differ from a rule-selected accepted ref;
	// both must be typed, but recovery never substitutes one for the other.
	defaultRef := receipt.Clone()
	defaultRef.FullRef = p.Source.FullRef
	if defaultRef.Validate() != nil || plan.PlanHash != model.PipelineSourceConfigDigest(p, plan) {
		return invalid()
	}
	environments := map[string]bool{}
	for _, stage := range p.Stages {
		if stage.EnvironmentID != "" {
			if model.IsRegistryCredentialEnvironmentID(stage.EnvironmentID) {
				return invalid()
			}
			environments[stage.EnvironmentID] = true
		}
	}
	if len(environments) != len(plan.EnvironmentVersions) {
		return invalid()
	}
	for id := range environments {
		if version, ok := plan.EnvironmentVersions[id]; !ok || version < 0 {
			return invalid()
		}
	}
	if len(plan.InputFiles) == 0 || len(plan.InputFiles) > 384 {
		return invalid()
	}
	for i, input := range plan.InputFiles {
		if input.Path == "." || path.IsAbs(input.Path) || path.Clean(input.Path) != input.Path || strings.HasPrefix(input.Path, "~") || strings.ContainsAny(input.Path, "\\:\x00\r\n") || strings.Contains(input.Path, "${") || strings.Contains(input.Path, "{{") || i > 0 && plan.InputFiles[i-1].Path >= input.Path || input.Present && !recoverySourceHex(input.SHA256) || !input.Present && input.SHA256 != "" {
			return invalid()
		}
		for _, part := range strings.Split(input.Path, "/") {
			if part == ".." || part == ".git" {
				return invalid()
			}
		}
	}
	inputs, err := json.Marshal(plan.InputFiles)
	if err != nil {
		return invalid()
	}
	envelope := sha256.Sum256(inputs)
	if plan.EnvelopeHash != hex.EncodeToString(envelope[:]) {
		return invalid()
	}
	if proof := payload.Trigger; proof != nil {
		if proof.IntentID == "" || len(proof.IntentID) > 128 || proof.RuleID == "" || len(proof.RuleID) > 128 || proof.RuleVersion < 1 || proof.TargetKind != "pipeline" || proof.TargetID != p.ID || proof.SourcePlanDigest != receipt.SourcePlanDigest || proof.EnvelopeHash != plan.EnvelopeHash {
			return invalid()
		}
	} else if plan.ReviewPolicy != "manual" {
		return invalid()
	}
	if run.Source != nil {
		if run.Source.CheckedOutAt != nil {
			return invalid()
		}
		if _, err = model.MergeRunSource(receipt, run.Source); err != nil {
			return invalid()
		}
	}
	return receipt, nil
}

func recoverySourceHex(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// The owning job is already locked by lifecycle transitions. Lock its run view
// before checking identity/lineage so workers cannot race an unchecked update.
func mergeRecoveredRunSource(ctx context.Context, tx *sql.Tx, payload *recoveryJobPayload, accepted *model.PipelineRunSource) error {
	var pipelineID, subject, email, token string
	var version int
	var groupsJSON, sourceJSON []byte
	err := tx.QueryRowContext(ctx, `SELECT pipeline_id,pipeline_version,started_by_user_sub,started_by_email,started_by_groups,started_by_token_hash,source FROM pipeline_runs WHERE id=$1 FOR UPDATE`, payload.RunID).Scan(&pipelineID, &version, &subject, &email, &groupsJSON, &token, &sourceJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // Deleted pipelines are handled without resurrection below.
	}
	if err != nil {
		return err
	}
	var groups []string
	var stored *model.PipelineRunSource
	if json.Unmarshal(groupsJSON, &groups) != nil || len(sourceJSON) > 0 && json.Unmarshal(sourceJSON, &stored) != nil || stored.Validate() != nil || payload.PipelineID != "" && pipelineID != payload.PipelineID {
		return fmt.Errorf("recovered run differs from the accepted plan")
	}
	if run := payload.Run; run != nil && (version != run.PipelineVersion || subject != run.StartedByUserSub || email != run.StartedByEmail || token != run.StartedByTokenHash || !slices.Equal(groups, run.StartedByGroups)) {
		return fmt.Errorf("recovered run differs from the accepted plan")
	}
	if accepted == nil {
		if stored != nil {
			return fmt.Errorf("recovered run source has no accepted source plan")
		}
		return nil
	}
	merged, err := model.MergeRunSource(stored, accepted)
	if err != nil {
		return fmt.Errorf("recovered run source differs from the accepted plan")
	}
	encoded, err := marshalRunSource(merged)
	if err != nil {
		return fmt.Errorf("recovered run has invalid source metadata")
	}
	_, err = tx.ExecContext(ctx, `UPDATE pipeline_runs SET source=$2 WHERE id=$1`, payload.RunID, encoded)
	return err
}
