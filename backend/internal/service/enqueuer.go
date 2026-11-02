package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// JobQueueEnqueuer implements the handler.JobEnqueuer interface by
// inserting a pipeline-run job into the durable queue. The matching
// worker (JobQueueRunner.Handle) picks it up via Dequeue.
//
// Concurrency-key is set to "pipeline:<id>" so two simultaneous
// triggers for the same pipeline are serialised at the queue layer
// (only one in 'running' at a time). Concurrent runs of *different*
// pipelines are unaffected.
type JobQueueEnqueuer struct {
	store     jobqueue.Store
	pipelines store.PipelineStore
	// MaxAttempts is retained for source compatibility. Deployments are
	// deliberately limited to one attempt; ambiguous effects need review.
	MaxAttempts         int
	PrepareSecrets      func(context.Context, *model.Pipeline) (*PipelineSecretPlan, error)
	PrepareRegistry     func(context.Context, *model.Pipeline) (*model.RegistryAuthorityPlan, error)
	RequireRegistryPins bool
	PrepareSource       func(context.Context, *model.Pipeline) (*model.PipelineSourcePlan, error)
	AdmissionGuard      store.NativeTriggerAdmissionGuard
}

// NewJobQueueEnqueuer wraps a jobqueue.Store.
func NewJobQueueEnqueuer(jobs jobqueue.Store, pipelines ...store.PipelineStore) *JobQueueEnqueuer {
	e := &JobQueueEnqueuer{store: jobs, MaxAttempts: 1}
	if len(pipelines) > 0 {
		e.pipelines = pipelines[0]
	}
	return e
}

// EnqueueRun implements handler.JobEnqueuer.
func (e *JobQueueEnqueuer) EnqueueRun(ctx context.Context, pipelineID, runID string) error {
	plan := runJobPayload{PipelineID: pipelineID, RunID: runID}
	if e.pipelines != nil {
		snapshot, err := e.pipelines.Get(ctx, pipelineID)
		if err != nil {
			return fmt.Errorf("enqueuer: snapshot pipeline: %w", err)
		}
		plan.Pipeline = snapshot
		// Scheduled work has no HTTP-created run. Persist enough input for
		// the worker to create the view without consulting a newer definition.
		plan.Run = pendingPipelineRun(snapshot, runID)
	}
	return e.enqueue(ctx, plan)
}

// EnqueuePlannedRun accepts the complete run before its queryable view is
// inserted. A process crash after this durable write cannot strand accepted
// work: both the worker and lifecycle recovery can reconstruct the same view.
// The supplied actor is part of the snapshot; a later worker never invents it.
func (e *JobQueueEnqueuer) EnqueuePlannedRun(ctx context.Context, pipeline *model.Pipeline, run *model.PipelineRun) error {
	if pipeline == nil || run == nil || pipeline.ID == "" || run.ID == "" || run.PipelineID != pipeline.ID || run.PipelineVersion != pipeline.Version || run.Status != model.RunStatusPending || run.StartedAt != nil || run.FinishedAt != nil {
		return fmt.Errorf("enqueuer: invalid pending run snapshot")
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	return e.enqueue(ctx, runJobPayload{PipelineID: pipeline.ID, RunID: run.ID, Pipeline: pipeline, Run: run})
}

func pendingPipelineRun(pipeline *model.Pipeline, runID string) *model.PipelineRun {
	run := &model.PipelineRun{ID: runID, PipelineID: pipeline.ID, PipelineVersion: pipeline.Version, Status: model.RunStatusPending, Variables: pipeline.Variables, CreatedAt: time.Now().UTC(), StageRuns: make([]model.StageRun, 0, len(pipeline.Stages))}
	for _, stage := range pipeline.Stages {
		run.StageRuns = append(run.StageRuns, model.StageRun{StageID: stage.ID, Status: model.RunStatusPending})
	}
	return run
}

func (e *JobQueueEnqueuer) enqueue(ctx context.Context, plan runJobPayload) error {
	if e == nil || e.store == nil || plan.PipelineID == "" || plan.RunID == "" {
		return fmt.Errorf("enqueuer: durable queue and run identity are required")
	}
	if plan.Pipeline != nil && (plan.Pipeline.Source != nil || e.PrepareRegistry != nil || e.RequireRegistryPins) {
		if e.AdmissionGuard == nil {
			return fmt.Errorf("native admission serialization is unavailable")
		}
		return e.AdmissionGuard.WithNativeTriggerAdmission(ctx, "run:"+plan.RunID, func(ctx context.Context) error {
			return e.enqueuePlan(ctx, plan)
		})
	}
	return e.enqueuePlan(ctx, plan)
}

func (e *JobQueueEnqueuer) enqueuePlan(ctx context.Context, plan runJobPayload) error {
	if found, err := e.existingPlan(ctx, &plan); found || err != nil {
		return err
	}
	if err := e.prepareRegistryPlan(ctx, &plan); err != nil {
		return err
	}
	if err := e.prepareSourcePlan(ctx, &plan); err != nil {
		return err
	}
	if err := e.prepareSecretPlan(ctx, &plan); err != nil {
		return err
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("enqueuer: marshal payload: %w", err)
	}
	_, err = e.store.Enqueue(ctx, JobKindPipelineRun, payload, jobqueue.EnqueueOptions{
		ID:             "run:" + plan.RunID,
		MaxAttempts:    1,
		ConcurrencyKey: "pipeline:" + plan.PipelineID,
	})
	if err != nil {
		// An accepted job is never replaced or repinned after an ambiguous
		// INSERT outcome. Recover the same immutable identity directly.
		if found, readErr := e.existingPlan(ctx, &plan); found || readErr != nil {
			return readErr
		}
	}
	return err
}

func (e *JobQueueEnqueuer) existingPlan(ctx context.Context, requested *runJobPayload) (bool, error) {
	j, err := e.store.Get(ctx, "run:"+requested.RunID)
	if errors.Is(err, jobqueue.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("accepted native receipt is unavailable")
	}
	var accepted runJobPayload
	if json.Unmarshal(j.Payload, &accepted) != nil || j.Kind != JobKindPipelineRun || accepted.PipelineID != requested.PipelineID || accepted.RunID != requested.RunID || accepted.Pipeline == nil || accepted.Run == nil || accepted.Pipeline.ID != accepted.PipelineID || accepted.Run.ID != accepted.RunID || accepted.Run.PipelineID != accepted.PipelineID {
		return true, fmt.Errorf("run identity conflicts with an accepted job")
	}
	if requested.Pipeline != nil && requested.Pipeline.Version != accepted.Pipeline.Version {
		return true, fmt.Errorf("run identity conflicts with an accepted pipeline version")
	}
	if requested.Pipeline != nil {
		want, err := json.Marshal(requested.Pipeline)
		got, oldErr := json.Marshal(accepted.Pipeline)
		if err != nil || oldErr != nil || string(want) != string(got) {
			return true, fmt.Errorf("run identity conflicts with accepted pipeline configuration")
		}
	}
	if requested.Run != nil && (requested.Run.StartedByUserSub != accepted.Run.StartedByUserSub || requested.Run.StartedByEmail != accepted.Run.StartedByEmail || requested.Run.StartedByTokenHash != accepted.Run.StartedByTokenHash || !reflect.DeepEqual(requested.Run.StartedByGroups, accepted.Run.StartedByGroups)) {
		return true, fmt.Errorf("run identity conflicts with an accepted actor")
	}
	if requested.Source != nil {
		want, err := model.PipelineSourcePlanDigest(requested.Source)
		got, oldErr := model.PipelineSourcePlanDigest(accepted.Source)
		if err != nil || oldErr != nil || want != got {
			return true, fmt.Errorf("run identity conflicts with accepted source")
		}
	}
	if !reflect.DeepEqual(requested.Trigger, accepted.Trigger) {
		return true, fmt.Errorf("run identity conflicts with an accepted trigger")
	}
	if accepted.Registry != nil && validateNativeRegistryPlan(accepted.Pipeline, accepted.Registry) != nil {
		return true, model.ErrInvalidRegistryAuthority
	}
	if e.RequireRegistryPins && accepted.Registry == nil {
		return true, ErrRegistryAuthorityRequired
	}
	return true, nil
}

func (e *JobQueueEnqueuer) prepareSourcePlan(ctx context.Context, plan *runJobPayload) error {
	if plan.Pipeline == nil || plan.Pipeline.Source == nil {
		if plan.Source != nil || plan.Trigger != nil {
			return fmt.Errorf("source metadata does not belong to this pipeline")
		}
		if plan.Run != nil && plan.Run.Source != nil {
			return fmt.Errorf("run source does not belong to this pipeline")
		}
		return nil
	}
	if plan.Source == nil {
		if plan.Trigger != nil || e.PrepareSource == nil {
			return fmt.Errorf("native source admission is unavailable")
		}
		var err error
		plan.Source, err = e.PrepareSource(ctx, plan.Pipeline)
		if err != nil {
			return fmt.Errorf("native source could not be prepared")
		}
	}
	if err := ValidatePipelineSourcePlan(plan.Pipeline, plan.Source); err != nil {
		return fmt.Errorf("native source admission metadata is invalid")
	}
	if proof := plan.Trigger; proof != nil {
		digest, err := model.PipelineSourcePlanDigest(plan.Source)
		if err != nil || proof.IntentID == "" || proof.RuleID == "" || proof.RuleVersion < 1 || proof.TargetKind != "pipeline" || proof.TargetID != plan.PipelineID || proof.SourcePlanDigest != digest || proof.EnvelopeHash != plan.Source.EnvelopeHash {
			return fmt.Errorf("native source trigger proof is invalid")
		}
	} else if plan.Source.ReviewPolicy != "manual" {
		return fmt.Errorf("manual source admission requires a manual plan")
	}
	if plan.Run == nil {
		return fmt.Errorf("source admission requires an immutable run")
	}
	receipt, err := model.RunSourceFromAcceptedPlan(plan.Source)
	if err != nil {
		return fmt.Errorf("native source receipt is invalid")
	}
	plan.Run = plan.Run.Clone()
	plan.Run.Source = receipt
	return nil
}

// EnqueueSourceRun accepts the native automation's already reviewed exact source.
// The existing PostgreSQL admission hook atomically creates the intent receipt.
func (e *JobQueueEnqueuer) EnqueueSourceRun(ctx context.Context, pipeline *model.Pipeline, run *model.PipelineRun, source *model.PipelineSourcePlan, proof *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("atomic native trigger admission is unavailable")
	}
	atomic, ok := e.store.(interface{ AtomicTriggerAdmission() bool })
	if !ok || !atomic.AtomicTriggerAdmission() {
		return nil, fmt.Errorf("atomic native trigger admission is unavailable")
	}
	if pipeline == nil || run == nil || source == nil || proof == nil || run.ID == "" || run.PipelineID != pipeline.ID || run.PipelineVersion != pipeline.Version || run.Status != model.RunStatusPending || run.CreatedAt.IsZero() || run.StartedAt != nil || run.FinishedAt != nil {
		return nil, fmt.Errorf("invalid native trigger admission")
	}
	plan := runJobPayload{PipelineID: pipeline.ID, RunID: run.ID, Pipeline: pipeline, Run: run, Source: source, Trigger: proof}
	if err := e.enqueue(ctx, plan); err != nil {
		return nil, err
	}
	j, err := e.store.Get(ctx, "run:"+run.ID)
	if err != nil {
		return nil, fmt.Errorf("accepted native receipt is unavailable")
	}
	digest, err := jobqueue.PayloadDigest(j.Payload)
	if err != nil {
		return nil, err
	}
	return &model.TriggerReceipt{RunID: run.ID, JobID: j.ID, PipelineID: pipeline.ID, PlanDigest: digest}, nil
}

func (e *JobQueueEnqueuer) prepareSecretPlan(ctx context.Context, plan *runJobPayload) error {
	if plan.Pipeline == nil {
		return nil
	}
	if e.PrepareSecrets != nil {
		var err error
		plan.Secrets, err = e.PrepareSecrets(ctx, plan.Pipeline)
		return err
	}
	bindings, err := pipelineSecretBindings(plan.Pipeline)
	if err != nil {
		return err
	}
	if len(bindings) != 0 {
		return fmt.Errorf("pipeline secret admission is unavailable")
	}
	return nil
}
