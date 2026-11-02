package service

import (
	"context"
	"encoding/json"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
	"strings"
	"testing"
	"time"
)

type nativeAtomicFixture struct{ *jobqueue.Memory }

func (*nativeAtomicFixture) AtomicTriggerAdmission() bool { return true }

func nativeRuleFixture(t *testing.T, policy string) (*PipelineAutomation, *model.TriggerIntent, *model.PipelineSourcePlan, *nativeSourceFixture) {
	t.Helper()
	ctx := context.Background()
	preparer, p, f := nativeSourceSetup(t)
	inbox := memory.NewTriggerStore()
	a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: inbox}, Jobs: &nativeAtomicFixture{jobqueue.NewMemory()}, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }}
	r := &model.TriggerRule{ID: "native-rule", Name: "native main", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: policy}
	preview, err := a.Preview(ctx, r, p.Source.FullRef)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	preview.Rule.ReviewedBySubject = "administrator"
	preview.Rule.ReviewedAt = &now
	if err = inbox.PutRule(ctx, preview.Rule); err != nil {
		t.Fatal(err)
	}
	event := model.VerifiedGitEvent{Source: r.Source, DeliveryID: "native-delivery", Event: "push", FullRef: p.Source.FullRef, CommitSHA: f.commit, ObjectSHA: f.object, PayloadHash: strings.Repeat("c", 64)}
	delivery, _, err := a.Triggers.AcceptVerified(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	i, err := inbox.GetIntent(ctx, delivery.Decisions[0].IntentID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := preparer.Prepare(ctx, p, event.FullRef, policy)
	if err != nil {
		t.Fatal(err)
	}
	return a, i, plan, f
}

func TestNativeTriggerExactReviewAndImmutableTarget(t *testing.T) {
	ctx := context.Background()
	a, i, _, _ := nativeRuleFixture(t, "review")
	if i.Status != model.TriggerAwaitingReview || i.Rule.AppID != "" {
		t.Fatal(i)
	}
	if err := a.ApproveIntent(ctx, i, "reviewer"); err != nil {
		t.Fatal(err)
	}
	got, _ := a.Triggers.Store.GetIntent(ctx, i.ID)
	if got.Status != model.TriggerPending || got.Review == nil || got.Review.CommitSHA != i.Event.CommitSHA || got.Review.RuleVersion != i.Rule.Version || got.Review.ReviewerSubject != "reviewer" {
		t.Fatal(got)
	}
	if err := a.ApproveIntent(ctx, i, "other"); err == nil {
		t.Fatal("stale reviewer changed exact receipt")
	}
	r, _ := a.Triggers.Store.GetRule(ctx, i.Rule.ID)
	r.TargetID = "different-pipeline"
	a.Triggers.Store.PutRule(ctx, r)
	history := a.Triggers.Store.(store.TriggerHistoryStore)
	page, err := history.ListTriggerHistory(ctx, store.TriggerHistoryQuery{TargetKind: "pipeline", TargetID: i.Rule.TargetID})
	if err != nil || page.Total != 1 || page.Items[0].AppID != "" || page.Items[0].TargetID != i.Rule.TargetID {
		t.Fatal("native history followed current rule", page, err)
	}
}

func TestNativeTriggerRecoveryDoesNotRepinAcceptedSource(t *testing.T) {
	ctx := context.Background()
	a, i, plan, f := nativeRuleFixture(t, "automatic")
	p, _ := a.Preparer.Store.Pipelines.Get(ctx, i.Rule.TargetID)
	run := pendingPipelineRun(p, i.RunID)
	run.StartedByUserSub = i.Rule.ActorSubject
	run.StartedByEmail = "automation:" + i.Rule.ID
	sd, _ := PipelineSourceDigest(plan)
	proof := model.PipelineTriggerProof{IntentID: i.ID, RuleID: i.Rule.ID, RuleVersion: i.Rule.Version, TargetKind: "pipeline", TargetID: p.ID, EnvelopeHash: plan.EnvelopeHash, SourcePlanDigest: sd}
	payload, _ := json.Marshal(map[string]any{"pipelineId": p.ID, "runId": run.ID, "pipeline": p, "run": run, "source": plan, "trigger": proof})
	job, err := a.Jobs.Enqueue(ctx, "pipeline-run", payload, jobqueue.EnqueueOptions{ID: "run:" + run.ID, ConcurrencyKey: "pipeline:" + p.ID})
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := jobqueue.PayloadDigest(payload)
	if err = a.Triggers.Store.TransitionIntent(ctx, i.ID, i.Version, model.TriggerQueued, "deployment durably queued", &model.TriggerReceipt{RunID: run.ID, JobID: job.ID, PipelineID: p.ID, PlanDigest: digest}); err != nil {
		t.Fatal(err)
	}
	before := f.identities + f.revisions + f.checkouts
	a.Enqueue = func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
		t.Fatal("accepted job was re-enqueued")
		return nil, nil
	}
	f.commit = strings.Repeat("b", 40)
	if err = a.Dispatch(ctx, i); err != nil {
		t.Fatal(err)
	}
	if before != f.identities+f.revisions+f.checkouts {
		t.Fatal("accepted receipt invoked provider or secret repinning")
	}
}

func TestNativeTriggerNonAtomicQueueCannotDispatch(t *testing.T) {
	a, i, _, f := nativeRuleFixture(t, "automatic")
	a.Jobs = jobqueue.NewMemory()
	a.Enqueue = func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
		t.Fatal("non-atomic native queue called")
		return nil, nil
	}
	before := f.checkouts
	if err := a.Dispatch(context.Background(), i); err == nil || before != f.checkouts {
		t.Fatal("non-atomic native trigger prepared execution")
	}
}

func TestNativeTriggerRuleRejectsContradictionsAndImportedFlows(t *testing.T) {
	for _, edit := range []func(*model.TriggerRule){func(r *model.TriggerRule) { r.AppID = "app" }, func(r *model.TriggerRule) { r.TargetID = "vflow_imported" }, func(r *model.TriggerRule) { r.EnvironmentID = "guessed-production" }, func(r *model.TriggerRule) { r.TargetKind = "provider" }} {
		a, i, _, _ := nativeRuleFixture(t, "automatic")
		_ = a
		r := store.CloneTrigger(&i.Rule)
		edit(r)
		if ValidateTriggerRule(r) == nil {
			t.Fatal("contradictory/native-imported trigger accepted")
		}
	}
}
