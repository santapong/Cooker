package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// SyncTriggerAdmission is called inside the job INSERT transaction. A queued
// receipt and the work it identifies cannot become visible independently.
func SyncTriggerAdmission(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
	if j.Kind == "pipeline-run" {
		return SyncPipelineTriggerAdmission(ctx, tx, j)
	}
	if j.Kind != "app-deploy" {
		return nil
	}
	var p struct {
		RunID   string             `json:"runId"`
		Commit  string             `json:"commit"`
		App     *model.App         `json:"app"`
		Actor   *model.PipelineRun `json:"actor"`
		Trigger *struct {
			IntentID     string `json:"intentId"`
			RuleID       string `json:"ruleId"`
			RuleVersion  int    `json:"ruleVersion"`
			EnvelopeHash string `json:"envelopeHash"`
		} `json:"trigger"`
	}
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return err
	}
	if p.Trigger == nil {
		return nil
	}
	r, err := scanTriggerRule(tx.QueryRowContext(ctx, `SELECT body,version,created_at,updated_at FROM trigger_rules WHERE id=$1 FOR SHARE`, p.Trigger.RuleID))
	if err != nil {
		return err
	}
	i, err := scanTriggerIntent(tx.QueryRowContext(ctx, `SELECT `+triggerIntentColumns+` FROM trigger_intents WHERE id=$1 FOR UPDATE`, p.Trigger.IntentID))
	if err != nil {
		return err
	}
	if p.App == nil || j.ID != "app-run:"+p.RunID || j.ConcurrencyKey != "app:"+p.App.ID {
		return fmt.Errorf("trigger job identity or target concurrency scope changed: %w", store.ErrConflict)
	}
	if i.Status != model.TriggerPending || !r.Enabled || r.Version != p.Trigger.RuleVersion || i.Rule.Version != r.Version || i.Rule.ID != r.ID || p.App == nil || p.Actor == nil || i.Rule.AppID != p.App.ID || r.ActorSubject != p.Actor.StartedByUserSub || r.AppVersion != p.App.Version || i.RunID != p.RunID || i.Event.CommitSHA != p.Commit || r.ApprovedEnvelopeHash != p.Trigger.EnvelopeHash {
		return fmt.Errorf("trigger changed before queue admission: %w", store.ErrConflict)
	}
	digest, err := jobqueue.PayloadDigest(j.Payload)
	if err != nil {
		return err
	}
	// ComposePipelineID(run,app,0) is the public root graph identity.
	receipt := model.TriggerReceipt{RunID: p.RunID, JobID: j.ID, PipelineID: "", PlanDigest: digest}
	// Match the service's existing deterministic graph namespace, without an
	// import cycle from persistence back into the service layer.
	receipt.PipelineID = "app-" + p.App.ID + "-" + p.RunID
	body, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE trigger_intents SET status='queued',reason='deployment durably queued',receipt=$2,version=version+1,updated_at=NOW() WHERE id=$1`, i.ID, body)
	return err
}
