package service

import (
	"context"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func TestAppRegistryCanaryPromotionRejectsUnsupportedTransportButPreservesAbort(t *testing.T) {
	for _, mode := range []string{"manual", "sweeper"} {
		t.Run(mode, func(t *testing.T) {
			q, app, values, _ := appRegistryQueueFixture(t)
			app.DeployTarget.Kind = model.DeployTargetKubernetes
			weighted := &fakeWeighted{}
			now := time.Now()
			due := now.Add(-time.Minute)
			svc, st := newCanaryFixture(t, app, weighted, WithCanaryClock(func() time.Time { return now }))
			svc.builder = q.Deployer
			c := &model.AppCanary{ID: "old-canary", AppID: app.ID, RunID: "old-run", Status: model.CanaryProgressing, StableImage: "registry.example.test/app:stable", CanaryImage: "registry.example.test/app:next", Weight: 20, AutoPromote: true, PromoteAfter: &due, StartedAt: due, UpdatedAt: due}
			if err := st.AppCanaries.Create(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			if mode == "manual" {
				if _, err := svc.Promote(context.Background(), app.ID); err == nil {
					t.Fatal("unsupported promotion accepted")
				}
			} else {
				svc.SweepAutoPromote(context.Background())
			}
			retained, err := st.AppCanaries.Get(context.Background(), c.ID)
			if err != nil || retained.Status != model.CanaryProgressing || weighted.count() != 0 || values.pins != 0 || values.reads != 0 {
				t.Fatal("forward mutation escaped unsupported registry transport", err)
			}
			// Abort remains the explicit recovery direction for already-running
			// legacy splits. It neither builds nor gains registry authority.
			if _, err := svc.Abort(context.Background(), app.ID, "operator recovery"); err != nil {
				t.Fatal(err)
			}
			if weighted.count() != 1 || weighted.lastWeight() != 0 || values.pins != 0 || values.reads != 0 {
				t.Fatal("safe abort gained build or registry authority")
			}
		})
	}
}
