package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"maps"
	"reflect"
	"strings"
	"time"
)

var errNativeSourceChanged = errors.New("native source ref changed")

// PipelineAutomation is deliberately separate from the App deploy facade.
// Root wiring supplies the durable native enqueue callback and authorization.
type PipelineAutomation struct {
	Triggers                 *TriggerService
	Preparer                 *PipelineSourcePreparer
	Jobs                     jobqueue.Store
	Enqueue                  func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error)
	Authorize                func(context.Context, *model.TriggerRule, *model.Pipeline) error
	RegistryPreflight        func(context.Context, *model.Pipeline) error
	RequireRegistryPreflight bool
}

func (a *PipelineAutomation) Preview(ctx context.Context, r *model.TriggerRule, fullRef string) (*TriggerPreview, error) {
	if a == nil || a.Preparer == nil || a.Preparer.Store == nil || r == nil {
		return nil, fmt.Errorf("native automation is unavailable")
	}
	kind, id := r.Target()
	if kind != "pipeline" || id == "" || r.AppID != "" {
		return nil, fmt.Errorf("native pipeline target is required")
	}
	p, err := a.Preparer.Store.Pipelines.Get(ctx, id)
	if err != nil || p.Source == nil {
		return nil, fmt.Errorf("source-bound native pipeline is unavailable")
	}
	if r.Source.ConnectionID != "" && r.Source.ConnectionID != p.Source.ConnectionID {
		return nil, fmt.Errorf("rule connection differs from pipeline source")
	}
	if r.Approval == "" {
		r.Approval = "review"
	}
	if err := a.checkRegistryPreflight(ctx, p); err != nil {
		return nil, err
	}
	plan, err := a.Preparer.Prepare(ctx, p, fullRef, r.Approval)
	if err != nil {
		return nil, err
	}
	r.Source = plan.Source
	r.SourceVersion = plan.ConnectionVersion
	r.PipelineVersion = p.Version
	r.EnvironmentVersions = plan.EnvironmentVersions
	r.ActorSubject = "cooker:trigger:" + r.ID
	r.ActorGroups = nil
	r.PlanHash = plan.PlanHash
	r.ApprovedEnvelopeHash = plan.EnvelopeHash
	r.ReviewedBySubject = ""
	r.ReviewedAt = nil
	if err = ValidateTriggerRule(r); err != nil {
		return nil, err
	}
	check := *r
	check.Enabled = true
	if ok, _ := TriggerRuleMatches(&check, model.VerifiedGitEvent{Source: r.Source, FullRef: plan.FullRef}); !ok {
		return nil, fmt.Errorf("preview ref must match native rule")
	}
	if a.Authorize == nil {
		return nil, fmt.Errorf("native automation authorization is unavailable")
	}
	if err = a.Authorize(ctx, r, p); err != nil {
		return nil, fmt.Errorf("native automation actor is not authorized")
	}
	paths := make([]string, 0, len(plan.InputFiles))
	for _, f := range plan.InputFiles {
		paths = append(paths, f.Path)
	}
	return &TriggerPreview{Rule: r, Envelope: &AutomationEnvelope{Hash: plan.EnvelopeHash, Commit: plan.CommitSHA, Files: paths}}, nil
}

func (a *PipelineAutomation) checkedRule(ctx context.Context, i *model.TriggerIntent) (*model.Pipeline, *model.TriggerRule, error) {
	if a == nil || a.Preparer == nil || a.Triggers == nil || a.Triggers.Store == nil || i == nil {
		return nil, nil, fmt.Errorf("native automation is unavailable")
	}
	r, err := a.Triggers.Store.GetRule(ctx, i.Rule.ID)
	if err != nil || !r.Enabled || r.Version != i.Rule.Version {
		return nil, nil, fmt.Errorf("native trigger rule changed")
	}
	kind, id := r.Target()
	ikind, iid := i.Rule.Target()
	if kind != "pipeline" || ikind != kind || iid != id || r.ActorSubject != "cooker:trigger:"+r.ID || len(r.ActorGroups) > 0 || r.ReviewedBySubject == "" || r.ReviewedAt == nil || r.ReviewedAt.IsZero() {
		return nil, nil, fmt.Errorf("native trigger authority changed")
	}
	p, err := a.Preparer.Store.Pipelines.Get(ctx, id)
	if err != nil || p.Version != r.PipelineVersion || p.Source == nil {
		return nil, nil, fmt.Errorf("native pipeline changed after review")
	}
	if err = ValidatePipelineSource(p); err != nil {
		return nil, nil, err
	}
	if !r.Source.SameRepository(i.Event.Source) || r.PlanHash != i.Rule.PlanHash || r.ApprovedEnvelopeHash != i.Rule.ApprovedEnvelopeHash || !maps.Equal(r.EnvironmentVersions, i.Rule.EnvironmentVersions) {
		return nil, nil, fmt.Errorf("native trigger configuration changed")
	}
	if a.Authorize == nil || a.Authorize(ctx, r, p) != nil {
		return nil, nil, fmt.Errorf("native automation actor is not authorized")
	}
	return p, r, nil
}

func (a *PipelineAutomation) prepareIntent(ctx context.Context, i *model.TriggerIntent) (*model.Pipeline, *model.PipelineSourcePlan, error) {
	p, r, err := a.checkedRule(ctx, i)
	if err != nil {
		return nil, nil, err
	}
	if err := a.checkRegistryPreflight(ctx, p); err != nil {
		return nil, nil, err
	}
	plan, err := a.Preparer.Prepare(ctx, p, i.Event.FullRef, r.Approval)
	if err != nil {
		return nil, nil, err
	}
	if !sameAcceptedRevision(i.Event, SourceRevision{FullRef: plan.FullRef, Commit: plan.CommitSHA, Object: plan.ObjectSHA}) {
		return nil, nil, errNativeSourceChanged
	}
	if !plan.Source.SameRepository(r.Source) || plan.ConnectionVersion != r.SourceVersion || plan.PlanHash != r.PlanHash || plan.EnvelopeHash != r.ApprovedEnvelopeHash || !maps.Equal(plan.EnvironmentVersions, r.EnvironmentVersions) {
		return nil, nil, fmt.Errorf("native source configuration requires review")
	}
	return p, plan, nil
}

func (a *PipelineAutomation) Dispatch(ctx context.Context, i *model.TriggerIntent) error {
	if i == nil || i.Status != model.TriggerPending {
		return nil
	}
	if a == nil || a.Triggers == nil || a.Triggers.Store == nil {
		return fmt.Errorf("native automation is unavailable")
	}
	guard, ok := a.Triggers.Store.(store.NativeTriggerAdmissionGuard)
	if !ok {
		return fmt.Errorf("native admission serialization is unavailable")
	}
	return guard.WithNativeTriggerAdmission(ctx, i.ID, func(admissionCtx context.Context) error { return a.dispatch(admissionCtx, i) })
}

func (a *PipelineAutomation) dispatch(ctx context.Context, i *model.TriggerIntent) error {
	if i == nil || i.Status != model.TriggerPending {
		return nil
	}
	if a == nil || a.Jobs == nil || a.Enqueue == nil {
		return fmt.Errorf("durable native automation queue is unavailable")
	}
	atomic, ok := a.Jobs.(interface{ AtomicTriggerAdmission() bool })
	if !ok || !atomic.AtomicTriggerAdmission() {
		return fmt.Errorf("atomic native trigger admission is unavailable")
	}
	// Recover before source/provider/secret repinning. Never replace an admitted job.
	if job, err := a.Jobs.Get(ctx, "run:"+i.RunID); err == nil {
		return a.recoverReceipt(ctx, i, job)
	} else if !errors.Is(err, jobqueue.ErrNotFound) {
		return err
	}
	p, plan, err := a.prepareIntent(ctx, i)
	if err != nil {
		status := model.TriggerAwaitingReview
		reason := "native source or configuration requires review"
		if errors.Is(err, errNativeSourceChanged) && strings.HasPrefix(i.Event.FullRef, "refs/heads/") {
			status = model.TriggerSuperseded
			reason = "source ref changed; this delivery will not deploy a different revision"
		}
		return a.Triggers.Store.TransitionIntent(ctx, i.ID, i.Version, status, reason, nil)
	}
	if i.Rule.Approval != "automatic" {
		if i.Review == nil || i.Review.IntentVersion != i.Version-1 || i.Review.RuleVersion != i.Rule.Version || i.Review.PipelineVersion != p.Version || i.Review.CommitSHA != plan.CommitSHA || i.Review.ObjectSHA != plan.ObjectSHA || i.Review.PlanHash != plan.PlanHash || i.Review.EnvelopeHash != plan.EnvelopeHash {
			return fmt.Errorf("native intent has no exact review receipt")
		}
	}
	run := pendingPipelineRun(p, i.RunID)
	run.StartedByUserSub = i.Rule.ActorSubject
	run.StartedByEmail = "automation:" + i.Rule.ID
	run.StartedByGroups = nil
	digest, err := PipelineSourceDigest(plan)
	if err != nil {
		return err
	}
	proof := &model.PipelineTriggerProof{IntentID: i.ID, RuleID: i.Rule.ID, RuleVersion: i.Rule.Version, TargetKind: "pipeline", TargetID: p.ID, EnvelopeHash: plan.EnvelopeHash, SourcePlanDigest: digest}
	receipt, err := a.Enqueue(ctx, p, run, plan, proof)
	if err != nil {
		// A competing admission may have committed after our first lookup.
		// Recover its exact receipt directly; do not redo any preparation.
		if accepted, readErr := a.Jobs.Get(ctx, "run:"+i.RunID); readErr == nil {
			return a.recoverReceipt(ctx, i, accepted)
		}
		return err
	}
	current, err := a.Triggers.Store.GetIntent(ctx, i.ID)
	if err != nil {
		return err
	}
	if current.Status != model.TriggerQueued || !reflect.DeepEqual(current.Receipt, receipt) {
		return fmt.Errorf("native admission did not atomically record its receipt")
	}
	return nil
}

func (a *PipelineAutomation) ApproveIntent(ctx context.Context, i *model.TriggerIntent, reviewer string) error {
	if reviewer == "" || i == nil || i.Status != model.TriggerAwaitingReview {
		return store.ErrConflict
	}
	p, plan, err := a.prepareIntent(ctx, i)
	if err != nil {
		return err
	}
	s, ok := a.Triggers.Store.(store.TriggerReviewStore)
	if !ok {
		return fmt.Errorf("atomic native review is unavailable")
	}
	return s.ApproveReviewedIntent(ctx, i.ID, i.Version, &model.TriggerReview{ReviewerSubject: reviewer, ReviewedAt: time.Now().UTC(), IntentVersion: i.Version, RuleVersion: i.Rule.Version, PipelineVersion: p.Version, CommitSHA: plan.CommitSHA, ObjectSHA: plan.ObjectSHA, PlanHash: plan.PlanHash, EnvelopeHash: plan.EnvelopeHash})
}

// CheckRef repairs one explicitly selected missed ref through the existing
// verified inbox. It cannot authorize a changed pipeline or replace review.
func (a *PipelineAutomation) CheckRef(ctx context.Context, r *model.TriggerRule, fullRef string) (*model.TriggerDelivery, bool, error) {
	if a == nil || a.Preparer == nil || a.Preparer.Store == nil || a.Triggers == nil || a.Triggers.Store == nil || r == nil {
		return nil, false, fmt.Errorf("native automation is unavailable")
	}
	kind, id := r.Target()
	if kind != "pipeline" || !r.Enabled || r.ReviewedBySubject == "" || r.ReviewedAt == nil || r.ReviewedAt.IsZero() || r.ActorSubject != "cooker:trigger:"+r.ID || len(r.ActorGroups) != 0 {
		return nil, false, fmt.Errorf("reviewed native rule is required")
	}
	current, err := a.Triggers.Store.GetRule(ctx, r.ID)
	if err != nil || !reflect.DeepEqual(current, r) {
		return nil, false, fmt.Errorf("native rule changed")
	}
	p, err := a.Preparer.Store.Pipelines.Get(ctx, id)
	if err != nil || p == nil || p.Source == nil || p.Version != r.PipelineVersion {
		return nil, false, fmt.Errorf("native pipeline changed after review")
	}
	if a.Authorize == nil || a.Authorize(ctx, r, p) != nil {
		return nil, false, fmt.Errorf("native automation actor is not authorized")
	}
	if ok, _ := TriggerRuleMatches(r, model.VerifiedGitEvent{Source: r.Source, FullRef: fullRef}); !ok {
		return nil, false, fmt.Errorf("ref is outside the reviewed rule")
	}
	if err := a.checkRegistryPreflight(ctx, p); err != nil {
		return nil, false, err
	}
	plan, err := a.Preparer.Prepare(ctx, p, fullRef, r.Approval)
	if err != nil || !plan.Source.SameRepository(r.Source) || plan.ConnectionVersion != r.SourceVersion || plan.PlanHash != r.PlanHash || plan.EnvelopeHash != r.ApprovedEnvelopeHash || !maps.Equal(plan.EnvironmentVersions, r.EnvironmentVersions) {
		return nil, false, fmt.Errorf("native source or configuration requires review")
	}
	encoded, err := json.Marshal([]string{r.Source.ConnectionID, r.Source.RepositoryID, plan.FullRef, plan.CommitSHA, plan.ObjectSHA})
	if err != nil {
		return nil, false, err
	}
	hash := sha256.Sum256(encoded)
	sum := hex.EncodeToString(hash[:])
	event := model.VerifiedGitEvent{Source: plan.Source, DeliveryID: "reconcile:" + r.ID + ":" + fmt.Sprint(r.Version) + ":" + sum, Event: "push", FullRef: plan.FullRef, CommitSHA: plan.CommitSHA, PayloadHash: sum}
	if strings.HasPrefix(plan.FullRef, "refs/tags/") {
		event.ObjectSHA = plan.ObjectSHA
	}
	return a.Triggers.AcceptVerified(ctx, event)
}

func (a *PipelineAutomation) recoverReceipt(ctx context.Context, i *model.TriggerIntent, job *jobqueue.Job) error {
	var plan struct {
		PipelineID string                      `json:"pipelineId"`
		RunID      string                      `json:"runId"`
		Source     *model.PipelineSourcePlan   `json:"source"`
		Trigger    *model.PipelineTriggerProof `json:"trigger"`
	}
	kind, target := i.Rule.Target()
	if json.Unmarshal(job.Payload, &plan) != nil || plan.Trigger == nil || plan.Source == nil || job.Kind != "pipeline-run" || job.ID != "run:"+i.RunID || job.ConcurrencyKey != "pipeline:"+target || kind != "pipeline" || plan.PipelineID != target || plan.Source.PipelineID != target || plan.Trigger.TargetKind != kind || plan.Trigger.TargetID != target || plan.Trigger.IntentID != i.ID || plan.Trigger.RuleID != i.Rule.ID || plan.Trigger.RuleVersion != i.Rule.Version || plan.RunID != i.RunID || !sameAcceptedRevision(i.Event, SourceRevision{FullRef: plan.Source.FullRef, Commit: plan.Source.CommitSHA, Object: plan.Source.ObjectSHA}) || !plan.Source.Source.SameRepository(i.Event.Source) || plan.Source.PlanHash != i.Rule.PlanHash || plan.Source.EnvelopeHash != i.Rule.ApprovedEnvelopeHash || plan.Trigger.EnvelopeHash != plan.Source.EnvelopeHash {
		return fmt.Errorf("accepted native job identity changed")
	}
	sourceDigest, err := PipelineSourceDigest(plan.Source)
	if err != nil || sourceDigest != plan.Trigger.SourcePlanDigest {
		return fmt.Errorf("accepted native source plan digest changed")
	}
	digest, err := jobqueue.PayloadDigest(job.Payload)
	if err != nil {
		return err
	}
	receipt := &model.TriggerReceipt{RunID: i.RunID, JobID: job.ID, PipelineID: plan.PipelineID, PlanDigest: digest}
	current, err := a.Triggers.Store.GetIntent(ctx, i.ID)
	if err != nil {
		return err
	}
	if current.Status != model.TriggerQueued || !reflect.DeepEqual(current.Receipt, receipt) {
		return fmt.Errorf("native job exists without its matching atomic trigger receipt")
	}
	return nil
}

// ValidateQueued runs before any worker checkout or native execution. Accepted
// payload digest and current trusted authority remain separate checks.
func (a *PipelineAutomation) ValidateQueued(ctx context.Context, job *jobqueue.Job, p *model.Pipeline, run *model.PipelineRun, source *model.PipelineSourcePlan, proof *model.PipelineTriggerProof) error {
	if proof == nil || source == nil || p == nil || run == nil || job == nil {
		return fmt.Errorf("native trigger proof is required")
	}
	i, err := a.Triggers.Store.GetIntent(ctx, proof.IntentID)
	if err != nil {
		return err
	}
	current, r, err := a.checkedRule(ctx, i)
	if err != nil {
		return err
	}
	kind, id := r.Target()
	digest, err := jobqueue.PayloadDigest(job.Payload)
	if err != nil {
		return err
	}
	sourceDigest, err := PipelineSourceDigest(source)
	if err != nil {
		return err
	}
	if i.Status != model.TriggerQueued || i.Receipt == nil || i.Receipt.PlanDigest != digest || i.Receipt.JobID != job.ID || i.Receipt.RunID != run.ID || i.Receipt.PipelineID != p.ID || kind != proof.TargetKind || id != proof.TargetID || id != p.ID || proof.RuleID != r.ID || proof.RuleVersion != r.Version || proof.SourcePlanDigest != sourceDigest || proof.EnvelopeHash != source.EnvelopeHash || source.PlanHash != r.PlanHash || source.EnvelopeHash != r.ApprovedEnvelopeHash || !sameAcceptedRevision(i.Event, SourceRevision{FullRef: source.FullRef, Commit: source.CommitSHA, Object: source.ObjectSHA}) || run.StartedByUserSub != r.ActorSubject || run.StartedByEmail != "automation:"+r.ID || len(run.StartedByGroups) > 0 || run.StartedByTokenHash != "" || model.PipelineSourceConfigDigest(current, source) != model.PipelineSourceConfigDigest(p, source) {
		return fmt.Errorf("accepted native job or authority changed")
	}
	if err := a.checkRegistryPreflight(ctx, current); err != nil {
		return err
	}
	return a.Preparer.Validate(ctx, p, source, true)
}
