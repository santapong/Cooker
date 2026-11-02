package service

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

// reconstructibleNativeRun is a deliberately stronger heartbeat-only check of
// the immutable pending run/source view. It neither creates a run nor authorizes execution;
// the runner still validates current source, secrets and registry authority.
// The heartbeat store's missing-row result must precede use of this predicate.
func reconstructibleNativeRun(job *jobqueue.Job) bool {
	if job == nil || job.Kind != JobKindPipelineRun {
		return false
	}
	var payload runJobPayload
	if json.Unmarshal(job.Payload, &payload) != nil || payload.Pipeline == nil || payload.Run == nil {
		return false
	}
	p, r := payload.Pipeline, payload.Run
	nativeID := func(id string) bool {
		return (model.RegistrySubject{Kind: "pipeline", ID: id}).Validate() == nil && !strings.HasPrefix(id, "vapp_") && !strings.HasPrefix(id, "vflow_")
	}
	if !nativeID(p.ID) || !nativeID(r.ID) || job.ID != "run:"+r.ID || p.ID != payload.PipelineID || r.ID != payload.RunID || r.PipelineID != p.ID || p.Version < 1 || r.PipelineVersion != p.Version || r.Status != model.RunStatusPending || r.CreatedAt.IsZero() || r.StartedAt != nil || r.FinishedAt != nil || r.HeartbeatAt != nil || r.Error != "" || len(r.EnvironmentStatuses) != 0 || len(p.Stages) > 256 || len(r.StageRuns) != len(p.Stages) {
		return false
	}
	stages := make(map[string]bool, len(p.Stages))
	for _, stage := range p.Stages {
		if stage.ID == "" || stages[stage.ID] || stage.Config.ReviewOnly {
			return false
		}
		stages[stage.ID] = true
	}
	for _, stage := range r.StageRuns {
		if !stages[stage.StageID] || stage.Status != model.RunStatusPending || stage.StartedAt != nil || stage.FinishedAt != nil || stage.Logs != "" || stage.Error != "" || len(stage.Artifacts) != 0 || len(stage.Outputs) != 0 {
			return false
		}
		delete(stages, stage.StageID)
	}
	if len(stages) != 0 {
		return false
	}
	if payload.Registry != nil && validateNativeRegistryPlan(p, payload.Registry) != nil {
		return false
	}
	if p.Source == nil {
		return payload.Source == nil && payload.Trigger == nil && r.Source == nil
	}
	if ValidatePipelineSourcePlan(p, payload.Source) != nil || r.Source == nil || r.Source.CheckedOutAt != nil {
		return false
	}
	receipt, err := model.RunSourceFromAcceptedPlan(payload.Source)
	if err != nil || !reflect.DeepEqual(r.Source, receipt) {
		return false
	}
	if proof := payload.Trigger; proof != nil {
		return proof.IntentID != "" && proof.RuleID != "" && proof.RuleVersion > 0 && proof.TargetKind == "pipeline" && proof.TargetID == p.ID && proof.EnvelopeHash == payload.Source.EnvelopeHash && proof.SourcePlanDigest == receipt.SourcePlanDigest
	}
	return payload.Source.ReviewPolicy == "manual"
}
