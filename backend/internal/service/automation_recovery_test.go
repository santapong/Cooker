package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type recoveryInboxCounter struct {
	store.TriggerStore
	writes int
}

func (s *recoveryInboxCounter) RecordDelivery(ctx context.Context, d *model.TriggerDelivery, intents []*model.TriggerIntent) (*model.TriggerDelivery, bool, error) {
	s.writes++
	return s.TriggerStore.RecordDelivery(ctx, d, intents)
}

func TestAppMissedRefRequiresCurrentReviewedAuthority(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, *Automation, *automationSourceFixture, *model.TriggerRule) string
	}{
		{"disabled rule", func(t *testing.T, a *Automation, _ *automationSourceFixture, r *model.TriggerRule) string {
			r.Enabled = false
			if err := a.Triggers.SaveRule(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			return "refs/heads/main"
		}},
		{"stale supplied rule", func(t *testing.T, a *Automation, _ *automationSourceFixture, r *model.TriggerRule) string {
			updated := store.CloneTrigger(r)
			updated.Name = "new review"
			if err := a.Triggers.SaveRule(context.Background(), updated); err != nil {
				t.Fatal(err)
			}
			return "refs/heads/main"
		}},
		{"outside included ref", func(_ *testing.T, _ *Automation, _ *automationSourceFixture, _ *model.TriggerRule) string {
			return "refs/heads/other"
		}},
		{"different ref namespace", func(_ *testing.T, _ *Automation, _ *automationSourceFixture, _ *model.TriggerRule) string {
			return "refs/tags/main"
		}},
		{"revoked actor", func(_ *testing.T, a *Automation, _ *automationSourceFixture, _ *model.TriggerRule) string {
			a.Authorize = func(context.Context, *model.TriggerRule, *model.App) error {
				return errors.New("private-authority-diagnostic")
			}
			return "refs/heads/main"
		}},
		{"changed connection generation", func(t *testing.T, a *Automation, _ *automationSourceFixture, r *model.TriggerRule) string {
			c, err := a.Connections.Store.Get(context.Background(), r.Source.ConnectionID)
			if err != nil {
				t.Fatal(err)
			}
			c.Name = "replacement metadata"
			if err = a.Connections.Update(context.Background(), c, nil); err != nil {
				t.Fatal(err)
			}
			return "refs/heads/main"
		}},
		{"changed App version", func(t *testing.T, a *Automation, _ *automationSourceFixture, r *model.TriggerRule) string {
			app, err := a.Store.Apps.Get(context.Background(), r.AppID)
			if err != nil {
				t.Fatal(err)
			}
			app.Name = "new configuration"
			if err = a.Store.Apps.Update(context.Background(), app); err != nil {
				t.Fatal(err)
			}
			return "refs/heads/main"
		}},
		{"unreviewed machine groups", func(t *testing.T, a *Automation, _ *automationSourceFixture, r *model.TriggerRule) string {
			r.ActorGroups = []string{"admin"}
			if err := a.Triggers.SaveRule(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			return "refs/heads/main"
		}},
		{"unreviewed configuration hash", func(t *testing.T, a *Automation, _ *automationSourceFixture, r *model.TriggerRule) string {
			r.PlanHash = strings.Repeat("f", 64)
			if err := a.Triggers.SaveRule(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			return "refs/heads/main"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, source, i := automationReviewFixture(t, "branch")
			r, err := a.Triggers.Store.GetRule(context.Background(), i.Rule.ID)
			if err != nil {
				t.Fatal(err)
			}
			ref := tc.change(t, a, source, r)
			counter := &preflightAutomationSource{AutomationSource: source}
			a.Source = counter
			inbox := &recoveryInboxCounter{TriggerStore: a.Triggers.Store}
			a.Triggers.Store = inbox
			inspections := 0
			a.Inspect = func(context.Context, *model.App, map[string]string) (*AutomationEnvelope, error) {
				inspections++
				return nil, errors.New("private-inspection-diagnostic")
			}
			d, _, err := a.CheckRef(context.Background(), r, ref)
			if err == nil || d != nil {
				t.Fatal("unreviewed recovery reached the inbox")
			}
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("private callback error escaped")
			}
			if counter.identities != 0 || counter.revisions != 0 || inspections != 0 || inbox.writes != 0 {
				t.Fatal("unreviewed recovery touched source, inspection or inbox")
			}
		})
	}
}

func TestAppMissedRefRechecksConfigurationAfterInspection(t *testing.T) {
	for _, change := range []string{"envelope", "rule", "connection", "App", "wrong ref", "invalid commit", "revision failure"} {
		t.Run(change, func(t *testing.T) {
			a, source, i := automationReviewFixture(t, "branch")
			r, err := a.Triggers.Store.GetRule(context.Background(), i.Rule.ID)
			if err != nil {
				t.Fatal(err)
			}
			inbox := &recoveryInboxCounter{TriggerStore: a.Triggers.Store}
			a.Triggers.Store = inbox
			if change == "wrong ref" {
				source.revision.FullRef = "refs/heads/other"
			}
			if change == "invalid commit" {
				source.revision.Commit = "not-a-commit"
			}
			if change == "revision failure" {
				source.err = errors.New("private-provider-diagnostic")
			}
			a.Inspect = func(context.Context, *model.App, map[string]string) (*AutomationEnvelope, error) {
				switch change {
				case "envelope":
					return &AutomationEnvelope{Hash: strings.Repeat("f", 64)}, nil
				case "rule":
					updated := store.CloneTrigger(r)
					updated.Enabled = false
					if err := a.Triggers.SaveRule(context.Background(), updated); err != nil {
						t.Fatal(err)
					}
				case "connection":
					c, err := a.Connections.Store.Get(context.Background(), r.Source.ConnectionID)
					if err != nil {
						t.Fatal(err)
					}
					c.Name = "changed during inspection"
					if err = a.Connections.Update(context.Background(), c, nil); err != nil {
						t.Fatal(err)
					}
				case "App":
					app, err := a.Store.Apps.Get(context.Background(), r.AppID)
					if err != nil {
						t.Fatal(err)
					}
					app.Name = "changed during inspection"
					if err = a.Store.Apps.Update(context.Background(), app); err != nil {
						t.Fatal(err)
					}
				}
				return &AutomationEnvelope{Hash: r.ApprovedEnvelopeHash}, nil
			}
			d, _, err := a.CheckRef(context.Background(), r, "refs/heads/main")
			if err == nil || d != nil || inbox.writes != 0 {
				t.Fatal("changed recovery input reached the inbox")
			}
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("provider diagnostic escaped")
			}
		})
	}
}

func TestAppMissedRefUnchangedAuthorityRetainsNormalizedDedupe(t *testing.T) {
	for _, kind := range []string{"branch", "tag"} {
		t.Run(kind, func(t *testing.T) {
			a, source, i := automationReviewFixture(t, kind)
			r, err := a.Triggers.Store.GetRule(context.Background(), i.Rule.ID)
			if err != nil {
				t.Fatal(err)
			}
			d, duplicate, err := a.CheckRef(context.Background(), r, source.revision.FullRef)
			if err != nil || duplicate || len(d.Decisions) != 1 || d.Decisions[0].IntentID != i.ID || d.Decisions[0].Status != "duplicate" {
				t.Fatal("existing webhook intent was not retained")
			}
			repeated, duplicate, err := a.CheckRef(context.Background(), r, source.revision.FullRef)
			if err != nil || !duplicate || repeated.ID != d.ID || repeated.Decisions[0].IntentID != i.ID {
				t.Fatal("repeated recovery created another delivery or intent")
			}
		})
	}
}
