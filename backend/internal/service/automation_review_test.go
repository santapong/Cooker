package service

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

type automationSourceFixture struct {
	identity model.GitSourceIdentity
	revision SourceRevision
	err      error
}

func (s *automationSourceFixture) Identity(context.Context, *connection.Connection, string) (model.GitSourceIdentity, error) {
	return s.identity, nil
}
func (s *automationSourceFixture) Revision(context.Context, *connection.Connection, string, string) (SourceRevision, error) {
	return s.revision, s.err
}

type failedAdmissionFixture struct{ *jobqueue.Memory }

func (failedAdmissionFixture) AtomicTriggerAdmission() bool { return true }

// Only rejection paths use this memory capability fixture; successful atomic
// queue+receipt admission is proven separately against PostgreSQL.
func automationReviewFixture(t *testing.T, kind string) (*Automation, *automationSourceFixture, *model.TriggerIntent) {
	t.Helper()
	ctx := context.Background()
	codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	connections := &connection.Service{Store: connection.NewMemory(), Codec: codec}
	conn := &connection.Connection{Name: "source", Provider: "github", Enabled: true, Repositories: []string{"acme/shop"}}
	if err := connections.Create(ctx, conn, strings.Repeat("webhook-fixture-", 3)); err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	app := &model.App{ID: "app", Name: "shop", GitHubRepo: "acme/shop", Branch: "main", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Path: "compose.yaml"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	name := "main"
	if kind == "tag" {
		name = "release/v1"
	}
	source := &automationSourceFixture{identity: model.GitSourceIdentity{Provider: "github", Host: "github.com", ConnectionID: conn.ID, RepositoryID: "19", Repository: "acme/shop"}, revision: SourceRevision{FullRef: fullTriggerRef(kind, name), Commit: strings.Repeat("a", 40), Object: strings.Repeat("b", 40)}}
	a := &Automation{Store: st, Connections: connections, Source: source, Triggers: &TriggerService{Store: memory.NewTriggerStore()}, Inspect: func(context.Context, *model.App, map[string]string) (*AutomationEnvelope, error) {
		return &AutomationEnvelope{Hash: strings.Repeat("c", 64)}, nil
	}, Authorize: func(context.Context, *model.TriggerRule, *model.App) error { return nil }}
	r := &model.TriggerRule{ID: "rule", AppID: app.ID, Enabled: true, Source: source.identity, RefKind: kind, Match: "exact", Include: []string{name}, Approval: "automatic"}
	preview, err := a.Preview(ctx, r, source.revision.FullRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Triggers.SaveRule(ctx, preview.Rule); err != nil {
		t.Fatal(err)
	}
	event := model.VerifiedGitEvent{Source: source.identity, DeliveryID: "delivery", Event: "push", FullRef: source.revision.FullRef, CommitSHA: source.revision.Commit, PayloadHash: strings.Repeat("d", 64)}
	if kind == "tag" {
		event.ObjectSHA = source.revision.Object
	}
	delivery, _, err := a.Triggers.AcceptVerified(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := a.Triggers.Store.GetIntent(ctx, delivery.Decisions[0].IntentID)
	if err != nil || intent.Status != model.TriggerPending {
		t.Fatal(intent, err)
	}
	return a, source, intent
}
func queuedPlanForReview(t *testing.T, a *Automation, i *model.TriggerIntent) *AppDeployPlan {
	t.Helper()
	ctx := context.Background()
	app, err := a.checkedIntent(ctx, i)
	if err != nil {
		t.Fatal(err)
	}
	receipt := &model.TriggerReceipt{RunID: i.RunID, JobID: "app-run:" + i.RunID, PipelineID: ComposePipelineID(i.RunID, app.ID, 0), PlanDigest: strings.Repeat("e", 64)}
	if err := a.Triggers.Store.TransitionIntent(ctx, i.ID, i.Version, model.TriggerQueued, "test atomic boundary fixture", receipt); err != nil {
		t.Fatal(err)
	}
	return &AppDeployPlan{Version: 1, RunID: i.RunID, App: app, Commit: i.Event.CommitSHA, Actor: &model.PipelineRun{StartedByUserSub: i.Rule.ActorSubject}, Trigger: &TriggerExecution{IntentID: i.ID, RuleID: i.Rule.ID, RuleVersion: i.Rule.Version, EnvelopeHash: i.Rule.ApprovedEnvelopeHash}}
}
func TestQueuedAutomationRevalidatesSourceRevocationAndActor(t *testing.T) {
	cases := []struct {
		name, kind string
		change     func(*Automation, *automationSourceFixture, *model.TriggerIntent, *AppDeployPlan)
	}{
		{"branch advanced", "branch", func(a *Automation, s *automationSourceFixture, i *model.TriggerIntent, p *AppDeployPlan) {
			s.revision.Commit = strings.Repeat("f", 40)
		}},
		{"tag reannotated at same commit", "tag", func(a *Automation, s *automationSourceFixture, i *model.TriggerIntent, p *AppDeployPlan) {
			s.revision.Object = strings.Repeat("f", 40)
		}},
		{"provider unavailable", "branch", func(a *Automation, s *automationSourceFixture, i *model.TriggerIntent, p *AppDeployPlan) {
			s.err = errors.New("unavailable")
		}},
		{"machine authority revoked", "branch", func(a *Automation, s *automationSourceFixture, i *model.TriggerIntent, p *AppDeployPlan) {
			a.Authorize = func(context.Context, *model.TriggerRule, *model.App) error { return errors.New("revoked") }
		}},
		{"group injection", "branch", func(a *Automation, s *automationSourceFixture, i *model.TriggerIntent, p *AppDeployPlan) {
			p.Actor.StartedByGroups = []string{"admin"}
		}},
		{"token authority injection", "branch", func(a *Automation, s *automationSourceFixture, i *model.TriggerIntent, p *AppDeployPlan) {
			p.Actor.StartedByTokenHash = "arbitrary"
		}},
		{"connection disabled", "branch", func(a *Automation, s *automationSourceFixture, i *model.TriggerIntent, p *AppDeployPlan) {
			c, _ := a.Connections.Store.Get(context.Background(), i.Rule.Source.ConnectionID)
			c.Enabled = false
			if err := a.Connections.Update(context.Background(), c, nil); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, source, intent := automationReviewFixture(t, tc.kind)
			plan := queuedPlanForReview(t, a, intent)
			if err := a.ValidateQueued(context.Background(), plan); err != nil {
				t.Fatal("unchanged approved plan rejected", err)
			}
			tc.change(a, source, intent, plan)
			if err := a.ValidateQueued(context.Background(), plan); err == nil {
				t.Fatal("changed authority/source accepted at worker boundary")
			}
		})
	}
}
func TestDispatchRejectsMovedBranchAndSameCommitTagObject(t *testing.T) {
	for _, kind := range []string{"branch", "tag"} {
		t.Run(kind, func(t *testing.T) {
			a, source, intent := automationReviewFixture(t, kind)
			jobs := failedAdmissionFixture{jobqueue.NewMemory()}
			a.Queue = &AppDeployQueue{Jobs: jobs}
			if kind == "branch" {
				source.revision.Commit = strings.Repeat("f", 40)
			} else {
				source.revision.Object = strings.Repeat("f", 40)
			}
			if err := a.Dispatch(context.Background(), intent); err != nil {
				t.Fatal(err)
			}
			got, _ := a.Triggers.Store.GetIntent(context.Background(), intent.ID)
			want := model.TriggerSuperseded
			if kind == "tag" {
				want = model.TriggerAwaitingReview
			}
			if got.Status != want {
				t.Fatal("moved source was not stopped", got.Status)
			}
			if _, err := jobs.Get(context.Background(), "app-run:"+intent.RunID); !errors.Is(err, jobqueue.ErrNotFound) {
				t.Fatal("moved source queued a deployment", err)
			}
		})
	}
}
func TestCheckRefLogicallyDeduplicatesNormalizedTagWebhook(t *testing.T) {
	a, source, intent := automationReviewFixture(t, "tag")
	rule, err := a.Triggers.Store.GetRule(context.Background(), intent.Rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	delivery, _, err := a.CheckRef(context.Background(), rule, source.revision.FullRef)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Event.ObjectSHA != intent.Event.ObjectSHA || len(delivery.Decisions) != 1 || delivery.Decisions[0].IntentID != intent.ID || delivery.Decisions[0].Status != "duplicate" {
		t.Fatal("missed-change check duplicated accepted tag intent", delivery)
	}
	if _, err := a.Triggers.Store.GetIntent(context.Background(), intent.ID); errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}
