package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
	"time"
)

type preflightAutomationSource struct {
	AutomationSource
	identities, revisions int
}

func (s *preflightAutomationSource) Identity(ctx context.Context, c *connection.Connection, repo string) (model.GitSourceIdentity, error) {
	s.identities++
	return s.AutomationSource.Identity(ctx, c, repo)
}
func (s *preflightAutomationSource) Revision(ctx context.Context, c *connection.Connection, repo, ref string) (SourceRevision, error) {
	s.revisions++
	return s.AutomationSource.Revision(ctx, c, repo, ref)
}

func TestAppAutomationRegistryPreflightPrecedesSourceAndValueOperations(t *testing.T) {
	for _, mode := range []string{"required callback missing", "callback denied"} {
		for _, operation := range []string{"preview", "dispatch", "review", "check ref", "worker"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				a, source, i := automationReviewFixture(t, "branch")
				var plan *AppDeployPlan
				if operation == "worker" {
					plan = queuedPlanForReview(t, a, i)
				}
				sourceCounter := &preflightAutomationSource{AutomationSource: source}
				a.Source = sourceCounter
				envs := &preflightEnvironmentStore{EnvironmentStore: a.Store.Environments}
				a.Store.Environments = envs
				values, inspections, checks := 0, 0, 0
				a.ResolveEnvironment = func(context.Context, *model.App) (map[string]string, error) {
					values++
					return nil, errors.New(registryCanary)
				}
				a.Inspect = func(context.Context, *model.App, map[string]string) (*AutomationEnvelope, error) {
					inspections++
					return nil, errors.New(registryCanary)
				}
				a.RequireRegistryPreflight = true
				if mode == "callback denied" {
					a.RegistryPreflight = func(context.Context, *model.App) error { checks++; return errors.New(registryCanary) }
				}
				var err error
				switch operation {
				case "preview":
					_, err = a.Preview(context.Background(), store.CloneTrigger(&i.Rule), i.Event.FullRef)
				case "dispatch":
					a.Queue = &AppDeployQueue{Jobs: failedAdmissionFixture{jobqueue.NewMemory()}}
					err = a.Dispatch(context.Background(), i)
				case "review":
					err = a.ApproveIntent(context.Background(), i, "reviewer")
				case "check ref":
					_, _, err = a.CheckRef(context.Background(), store.CloneTrigger(&i.Rule), i.Event.FullRef)
				case "worker":
					err = a.ValidateQueued(context.Background(), plan)
				}
				if operation == "dispatch" {
					got, getErr := a.Triggers.Store.GetIntent(context.Background(), i.ID)
					if err != nil || getErr != nil || got.Status != model.TriggerAwaitingReview || strings.Contains(got.Reason, registryCanary) {
						t.Fatal("rejected fresh dispatch did not safely await review", err, getErr, got)
					}
				} else if err == nil || strings.Contains(err.Error(), registryCanary) {
					t.Fatal("unsafe preflight rejection", err)
				}
				if sourceCounter.identities+sourceCounter.revisions+values+inspections+envs.reads != 0 {
					t.Fatal("rejected preflight touched source/environment", sourceCounter, values, inspections, envs.reads)
				}
				want := 0
				if mode == "callback denied" {
					want = 1
				}
				if checks != want {
					t.Fatal("preflight count", checks, want)
				}
			})
		}
	}
}

func TestNativeAutomationRegistryPreflightPrecedesSourceAndValueOperations(t *testing.T) {
	for _, mode := range []string{"required callback missing", "callback denied"} {
		for _, operation := range []string{"preview", "dispatch", "review", "check ref", "worker"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				policy := "automatic"
				if operation == "review" {
					policy = "review"
				}
				a, i, plan, f := nativeRuleFixture(t, policy)
				var job *jobqueue.Job
				var p *model.Pipeline
				var run *model.PipelineRun
				var proof *model.PipelineTriggerProof
				if operation == "worker" {
					job, p, run, proof = nativeApprovalQueuedFixture(t, a, i, plan)
				}
				checks := 0
				a.RequireRegistryPreflight = true
				if mode == "callback denied" {
					a.RegistryPreflight = func(context.Context, *model.Pipeline) error { checks++; return errors.New(registryCanary) }
				}
				envs := &preflightEnvironmentStore{EnvironmentStore: a.Preparer.Store.Environments}
				a.Preparer.Store.Environments = envs
				before := f.identities + f.revisions + f.checkouts
				var err error
				switch operation {
				case "preview":
					_, err = a.Preview(context.Background(), store.CloneTrigger(&i.Rule), i.Event.FullRef)
				case "dispatch":
					a.Enqueue = func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
						t.Fatal("rejected preflight enqueued")
						return nil, nil
					}
					err = a.Dispatch(context.Background(), i)
				case "review":
					err = a.ApproveIntent(context.Background(), i, "reviewer")
				case "check ref":
					r, getErr := a.Triggers.Store.GetRule(context.Background(), i.Rule.ID)
					if getErr != nil {
						t.Fatal(getErr)
					}
					_, _, err = a.CheckRef(context.Background(), r, i.Event.FullRef)
				case "worker":
					err = a.ValidateQueued(context.Background(), job, p, run, plan, proof)
				}
				if operation == "dispatch" {
					got, getErr := a.Triggers.Store.GetIntent(context.Background(), i.ID)
					if err != nil || getErr != nil || got.Status != model.TriggerAwaitingReview || strings.Contains(got.Reason, registryCanary) {
						t.Fatal("rejected fresh dispatch did not safely await review", err, getErr, got)
					}
				} else if err == nil || strings.Contains(err.Error(), registryCanary) {
					t.Fatal("unsafe preflight rejection", err)
				}
				if before != f.identities+f.revisions+f.checkouts || envs.reads != 0 {
					t.Fatal("rejected preflight touched source/environment")
				}
				want := 0
				if mode == "callback denied" {
					want = 1
				}
				if checks != want {
					t.Fatal("preflight count", checks, want)
				}
			})
		}
	}
}

func TestAutomationSelectedRegistryCannotUseUnconfiguredLegacyPreflight(t *testing.T) {
	ctx := context.Background()
	a, _, i := automationReviewFixture(t, "branch")
	app, err := a.Store.Apps.Get(ctx, i.Rule.AppID)
	if err != nil {
		t.Fatal(err)
	}
	app.BuildPlan.RegistryConnectionIDs = []string{"registry"}
	if err = a.Store.Apps.Update(ctx, app); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Preview(ctx, store.CloneTrigger(&i.Rule), i.Event.FullRef); !errors.Is(err, ErrRegistryAuthorityRequired) {
		t.Fatal("App selected registry accepted without callback", err)
	}
	n, ni, _, f := nativeRuleFixture(t, "automatic")
	p, err := n.Preparer.Store.Pipelines.Get(ctx, ni.Rule.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	p.Stages[0].Config.RegistryConnectionIDs = []string{"registry"}
	if err = n.Preparer.Store.Pipelines.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	before := f.identities + f.revisions + f.checkouts
	if _, err = n.Preview(ctx, store.CloneTrigger(&ni.Rule), ni.Event.FullRef); !errors.Is(err, ErrRegistryAuthorityRequired) {
		t.Fatal("native selected registry accepted without callback", err)
	}
	if before != f.identities+f.revisions+f.checkouts {
		t.Fatal("selected native registry read source before callback")
	}
}

func TestAutomationReceiptRecoveryPrecedesChangedRegistryPreflight(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"required missing", "denied callback"} {
		t.Run("native/"+mode, func(t *testing.T) {
			a, i, source, f := nativeRuleFixture(t, "automatic")
			nativeApprovalQueuedFixture(t, a, i, source)
			a.RequireRegistryPreflight = true
			checks := 0
			if mode == "denied callback" {
				a.RegistryPreflight = func(context.Context, *model.Pipeline) error { checks++; return errors.New(registryCanary) }
			}
			before := f.identities + f.revisions + f.checkouts
			if err := a.Dispatch(ctx, i); err != nil {
				t.Fatal(err)
			}
			if checks != 0 || before != f.identities+f.revisions+f.checkouts {
				t.Fatal("accepted native receipt reran preflight/source")
			}
		})
		t.Run("app/"+mode, func(t *testing.T) {
			a, source, i := automationReviewFixture(t, "branch")
			app, err := a.checkedIntent(ctx, i)
			if err != nil {
				t.Fatal(err)
			}
			jobs := failedAdmissionFixture{jobqueue.NewMemory()}
			a.Queue = &AppDeployQueue{Jobs: jobs}
			payload, err := json.Marshal(AppDeployPlan{Version: 1, RunID: i.RunID, App: app, Commit: i.Event.CommitSHA})
			if err != nil {
				t.Fatal(err)
			}
			job, err := jobs.Enqueue(ctx, JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:" + i.RunID})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := receiptForPlan(job, app.ID, i.Event.CommitSHA)
			if err != nil {
				t.Fatal(err)
			}
			if err = a.Triggers.Store.TransitionIntent(ctx, i.ID, i.Version, model.TriggerQueued, "owned accepted fixture", &model.TriggerReceipt{RunID: receipt.RunID, JobID: receipt.JobID, PipelineID: receipt.PipelineID, PlanDigest: receipt.PlanDigest}); err != nil {
				t.Fatal(err)
			}
			counter := &preflightAutomationSource{AutomationSource: source}
			a.Source = counter
			a.RequireRegistryPreflight = true
			checks := 0
			if mode == "denied callback" {
				a.RegistryPreflight = func(context.Context, *model.App) error { checks++; return errors.New(registryCanary) }
			}
			if err = a.Dispatch(ctx, i); err != nil {
				t.Fatal(err)
			}
			if checks+counter.identities+counter.revisions != 0 {
				t.Fatal("accepted App receipt reran preflight/source")
			}
		})
	}
}

func TestAutomationCurrentRegistryRevocationStopsFreshSourcePreparation(t *testing.T) {
	ctx := context.Background()
	t.Run("native", func(t *testing.T) {
		preparer, p, f := nativeSourceSetup(t)
		registry := &model.RegistryConfig{ID: "registry", Name: "owned", URL: "https://ghcr.io/owned", AllowedPipelineIDs: []string{p.ID}}
		if err := preparer.Store.Registries.Create(ctx, registry); err != nil {
			t.Fatal(err)
		}
		p.Stages[0].Config.RegistryConnectionIDs = []string{registry.ID}
		if err := preparer.Store.Pipelines.Update(ctx, p); err != nil {
			t.Fatal(err)
		}
		p, err := preparer.Store.Pipelines.Get(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		authority := NewRegistryAuthorityService(preparer.Store, nil)
		a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: memory.NewTriggerStore()}, Jobs: &nativeAtomicFixture{jobqueue.NewMemory()}, RequireRegistryPreflight: true, RegistryPreflight: func(ctx context.Context, p *model.Pipeline) error {
			return CheckPipelineRegistryPreflight(ctx, authority, p)
		}, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }}
		r := &model.TriggerRule{ID: "registry-rule", Name: "owned", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "automatic"}
		if _, err = a.Preview(ctx, r, p.Source.FullRef); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		r.ReviewedBySubject, r.ReviewedAt = "admin", &now
		if err = a.Triggers.SaveRule(ctx, r); err != nil {
			t.Fatal(err)
		}
		d, _, err := a.Triggers.AcceptVerified(ctx, model.VerifiedGitEvent{Source: r.Source, DeliveryID: "registry-delivery", Event: "push", FullRef: p.Source.FullRef, CommitSHA: f.commit, PayloadHash: strings.Repeat("d", 64)})
		if err != nil {
			t.Fatal(err)
		}
		i, err := a.Triggers.Store.GetIntent(ctx, d.Decisions[0].IntentID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = preparer.Store.Registries.(store.RegistryGrantStore).UpdateGrants(ctx, registry.ID, registry.Version, nil, nil); err != nil {
			t.Fatal(err)
		}
		a.Enqueue = func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
			t.Fatal("revoked grant queued")
			return nil, nil
		}
		before := f.identities + f.revisions + f.checkouts
		if err = a.Dispatch(ctx, i); err != nil {
			t.Fatal(err)
		}
		got, _ := a.Triggers.Store.GetIntent(ctx, i.ID)
		if got.Status != model.TriggerAwaitingReview || before != f.identities+f.revisions+f.checkouts {
			t.Fatal("revoked native grant reached source")
		}
	})
	t.Run("app", func(t *testing.T) {
		a, s, initial := automationReviewFixture(t, "branch")
		app, err := a.Store.Apps.Get(ctx, initial.Rule.AppID)
		if err != nil {
			t.Fatal(err)
		}
		registry := &model.RegistryConfig{ID: "registry", Name: "owned", URL: "https://ghcr.io/owned", AllowedAppIDs: []string{app.ID}}
		if err = a.Store.Registries.Create(ctx, registry); err != nil {
			t.Fatal(err)
		}
		app.BuildPlan.RegistryConnectionIDs = []string{registry.ID}
		if err = a.Store.Apps.Update(ctx, app); err != nil {
			t.Fatal(err)
		}
		authority := NewRegistryAuthorityService(a.Store, nil)
		a.RequireRegistryPreflight = true
		a.RegistryPreflight = func(ctx context.Context, app *model.App) error { return CheckAppRegistryPreflight(ctx, authority, app) }
		a.Triggers.Store = memory.NewTriggerStore()
		r := store.CloneTrigger(&initial.Rule)
		r.ID = "registry-rule"
		r.Version = 0
		if _, err = a.Preview(ctx, r, s.revision.FullRef); err != nil {
			t.Fatal(err)
		}
		if err = a.Triggers.SaveRule(ctx, r); err != nil {
			t.Fatal(err)
		}
		d, _, err := a.Triggers.AcceptVerified(ctx, model.VerifiedGitEvent{Source: r.Source, DeliveryID: "registry-delivery", Event: "push", FullRef: s.revision.FullRef, CommitSHA: s.revision.Commit, PayloadHash: strings.Repeat("d", 64)})
		if err != nil {
			t.Fatal(err)
		}
		i, err := a.Triggers.Store.GetIntent(ctx, d.Decisions[0].IntentID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = a.Store.Registries.(store.RegistryGrantStore).UpdateGrants(ctx, registry.ID, registry.Version, nil, nil); err != nil {
			t.Fatal(err)
		}
		counter := &preflightAutomationSource{AutomationSource: s}
		a.Source = counter
		a.Queue = &AppDeployQueue{Jobs: failedAdmissionFixture{jobqueue.NewMemory()}}
		if err = a.Dispatch(ctx, i); err != nil {
			t.Fatal(err)
		}
		got, _ := a.Triggers.Store.GetIntent(ctx, i.ID)
		if got.Status != model.TriggerAwaitingReview || counter.identities+counter.revisions != 0 {
			t.Fatal("revoked App grant reached source")
		}
	})
}
