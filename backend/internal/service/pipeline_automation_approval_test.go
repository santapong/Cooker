package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

// Normalized branch deliveries intentionally omit ObjectSHA. The resolver
// still records its complete ref object in the immutable accepted source plan.
func nativeApprovalFixture(t *testing.T, policy, fullRef, eventObject, resolvedObject string) (*PipelineAutomation, *model.TriggerIntent, *model.PipelineSourcePlan, *nativeSourceFixture) {
	t.Helper()
	ctx := context.Background()
	preparer, p, f := nativeSourceSetup(t)
	f.object = resolvedObject
	inbox := memory.NewTriggerStore()
	a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: inbox}, Jobs: &nativeAtomicFixture{jobqueue.NewMemory()}, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }}
	kind, name := "branch", strings.TrimPrefix(fullRef, "refs/heads/")
	if strings.HasPrefix(fullRef, "refs/tags/") {
		kind, name = "tag", strings.TrimPrefix(fullRef, "refs/tags/")
	}
	r := &model.TriggerRule{ID: "native-approval-rule", Name: "native exact revision", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, RefKind: kind, Match: "exact", Include: []string{name}, Approval: policy}
	if _, err := a.Preview(ctx, r, fullRef); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	r.ReviewedBySubject, r.ReviewedAt = "rule-reviewer", &now
	if err := a.Triggers.SaveRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	event := model.VerifiedGitEvent{Source: r.Source, DeliveryID: "native-approval-delivery", Event: "push", FullRef: fullRef, CommitSHA: f.commit, ObjectSHA: eventObject, PayloadHash: strings.Repeat("c", 64)}
	delivery, _, err := a.Triggers.AcceptVerified(ctx, event)
	if err != nil || len(delivery.Decisions) != 1 || delivery.Decisions[0].IntentID == "" {
		t.Fatal("owned exact event did not produce intent", err)
	}
	i, err := inbox.GetIntent(ctx, delivery.Decisions[0].IntentID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := preparer.Prepare(ctx, p, fullRef, policy)
	if err != nil {
		t.Fatal(err)
	}
	return a, i, plan, f
}

func TestNativeApprovalTypedRefsUseAppRevisionSemantics(t *testing.T) {
	commit, tagObject := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct {
		name, ref, eventObject, resolvedObject string
		accept                                 bool
	}{
		{"branch-without-object", "refs/heads/main", "", commit, true},
		{"branch-with-object", "refs/heads/main", commit, commit, true},
		{"annotated-tag", "refs/tags/v1", tagObject, tagObject, true},
		{"lightweight-tag", "refs/tags/v1", commit, commit, true},
		{"missing-tag-object", "refs/tags/v1", "", tagObject, false},
		{"different-tag-same-commit", "refs/tags/v1", commit, tagObject, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, i, plan, _ := nativeApprovalFixture(t, "review", tc.ref, tc.eventObject, tc.resolvedObject)
			appMatches := sameAcceptedRevision(i.Event, SourceRevision{FullRef: plan.FullRef, Commit: plan.CommitSHA, Object: plan.ObjectSHA})
			if appMatches != tc.accept {
				t.Fatal("App typed-ref contract disagrees with fixture")
			}
			err := a.ApproveIntent(context.Background(), i, "server-reviewer")
			if (err == nil) != appMatches {
				t.Fatal("native approval differs from App typed-ref semantics", err)
			}
			current, err := a.Triggers.Store.GetIntent(context.Background(), i.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.accept {
				if current.Status != model.TriggerAwaitingReview || current.Review != nil {
					t.Fatal("rejected source received approval")
				}
				return
			}
			if current.Status != model.TriggerPending || current.Review == nil || current.Review.ReviewerSubject != "server-reviewer" || current.Review.CommitSHA != plan.CommitSHA || current.Review.ObjectSHA != plan.ObjectSHA || current.Review.IntentVersion != i.Version || current.Review.RuleVersion != i.Rule.Version || current.Review.PipelineVersion != plan.PipelineVersion || current.Review.PlanHash != plan.PlanHash || current.Review.EnvelopeHash != plan.EnvelopeHash {
				t.Fatal("exact resolved source and authority were not captured")
			}
			if err := a.ApproveIntent(context.Background(), i, "replacement-reviewer"); err == nil {
				t.Fatal("stale approval replaced accepted receipt")
			}
		})
	}
}

func TestNativeApprovalMovedRefStillRequiresExactAcceptedRevision(t *testing.T) {
	for _, kind := range []string{"branch", "tag"} {
		t.Run(kind, func(t *testing.T) {
			ref, eventObject, resolved := "refs/heads/main", "", strings.Repeat("a", 40)
			if kind == "tag" {
				ref, eventObject, resolved = "refs/tags/v1", strings.Repeat("b", 40), strings.Repeat("b", 40)
			}
			a, i, _, f := nativeApprovalFixture(t, "review", ref, eventObject, resolved)
			if kind == "branch" {
				f.commit, f.object = strings.Repeat("c", 40), strings.Repeat("c", 40)
			} else {
				// The peeled commit is unchanged; the annotated object moved.
				f.object = strings.Repeat("c", 40)
			}
			if err := a.ApproveIntent(context.Background(), i, "reviewer"); err == nil {
				t.Fatal("moved ref received approval")
			}
			current, _ := a.Triggers.Store.GetIntent(context.Background(), i.ID)
			if current.Review != nil || current.Status != model.TriggerAwaitingReview {
				t.Fatal("moved ref changed durable review")
			}
		})
	}
}

func nativeApprovalQueuedFixture(t *testing.T, a *PipelineAutomation, i *model.TriggerIntent, plan *model.PipelineSourcePlan) (*jobqueue.Job, *model.Pipeline, *model.PipelineRun, *model.PipelineTriggerProof) {
	t.Helper()
	ctx := context.Background()
	p, err := a.Preparer.Store.Pipelines.Get(ctx, i.Rule.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	run := pendingPipelineRun(p, i.RunID)
	run.StartedByUserSub, run.StartedByEmail = i.Rule.ActorSubject, "automation:"+i.Rule.ID
	sourceDigest, err := PipelineSourceDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	proof := &model.PipelineTriggerProof{IntentID: i.ID, RuleID: i.Rule.ID, RuleVersion: i.Rule.Version, TargetKind: "pipeline", TargetID: p.ID, EnvelopeHash: plan.EnvelopeHash, SourcePlanDigest: sourceDigest}
	payload, err := json.Marshal(map[string]any{"pipelineId": p.ID, "runId": run.ID, "pipeline": p, "run": run, "source": plan, "trigger": proof})
	if err != nil {
		t.Fatal(err)
	}
	job, err := a.Jobs.Enqueue(ctx, "pipeline-run", payload, jobqueue.EnqueueOptions{ID: "run:" + run.ID, ConcurrencyKey: "pipeline:" + p.ID})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := jobqueue.PayloadDigest(job.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Triggers.Store.TransitionIntent(ctx, i.ID, i.Version, model.TriggerQueued, "deployment durably queued", &model.TriggerReceipt{RunID: run.ID, JobID: job.ID, PipelineID: p.ID, PlanDigest: digest}); err != nil {
		t.Fatal(err)
	}
	a.Enqueue = func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
		t.Fatal("accepted receipt was repinned")
		return nil, nil
	}
	return job, p, run, proof
}

func TestNativeQueuedRevisionRecoveryAndValidationKeepTypedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, ref, eventObject, resolvedObject string
		accept                                 bool
	}{
		{"branch-without-object", "refs/heads/main", "", strings.Repeat("a", 40), true},
		{"annotated-tag", "refs/tags/v1", strings.Repeat("b", 40), strings.Repeat("b", 40), true},
		{"missing-tag-object", "refs/tags/v1", "", strings.Repeat("b", 40), false},
		{"different-tag-same-commit", "refs/tags/v1", strings.Repeat("c", 40), strings.Repeat("b", 40), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, i, plan, f := nativeApprovalFixture(t, "automatic", tc.ref, tc.eventObject, tc.resolvedObject)
			job, p, run, proof := nativeApprovalQueuedFixture(t, a, i, plan)
			before := f.identities + f.revisions + f.checkouts
			if err := a.Dispatch(context.Background(), i); (err == nil) != tc.accept {
				t.Fatal("receipt recovery lost typed identity", err)
			}
			if f.identities+f.revisions+f.checkouts != before {
				t.Fatal("receipt recovery read a provider or repinned source")
			}
			if err := a.ValidateQueued(context.Background(), job, p, run, plan, proof); (err == nil) != tc.accept {
				t.Fatal("worker validation lost typed identity", err)
			}
			if !tc.accept && f.identities+f.revisions+f.checkouts != before {
				t.Fatal("rejected tag identity reached provider or checkout")
			}
			if tc.accept {
				// Relaxing event normalization must not relax the later captured
				// source-plan freshness guard, including its resolved object.
				f.object = strings.Repeat("e", 40)
				if err := a.ValidateQueued(context.Background(), job, p, run, plan, proof); err == nil {
					t.Fatal("changed resolved object bypassed source freshness")
				}
			}
		})
	}
}

func TestNativeAndAppRevisionMatcherKeepsRefAndCommitExact(t *testing.T) {
	commit, object := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, eventRef := range []string{"refs/heads/release", "refs/tags/release"} {
		event := model.VerifiedGitEvent{FullRef: eventRef, CommitSHA: commit, ObjectSHA: object}
		for _, revision := range []SourceRevision{{FullRef: strings.Replace(eventRef, "/release", "/other", 1), Commit: commit, Object: object}, {FullRef: eventRef, Commit: strings.Repeat("c", 40), Object: object}, {FullRef: strings.Replace(eventRef, "refs/heads/", "refs/tags/", 1), Commit: commit, Object: object}} {
			if revision.FullRef == eventRef && revision.Commit == event.CommitSHA {
				continue
			}
			if sameAcceptedRevision(event, revision) {
				t.Fatal("different typed ref or commit matched")
			}
		}
	}
}
