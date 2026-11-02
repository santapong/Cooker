package service

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type unavailablePreflightEnvironment struct{ store.EnvironmentStore }

func (unavailablePreflightEnvironment) Get(context.Context, string) (*model.Environment, error) {
	return nil, errors.New("synthetic-environment-read-canary")
}

func TestAppEnvironmentPreflightHistoryAndRecovery(t *testing.T) {
	for _, selected := range []bool{false, true} {
		name := "empty"
		if selected {
			name = "selected"
		}
		t.Run(name, func(t *testing.T) {
			st, resolver, values, env := immutableSecretFixture(t)
			app := dockerfileScopedApp(env.ID)
			if selected {
				app.BuildPlan.ServiceEnvironment[DockerfileRuntimeService] = []string{"TOKEN", "DB_NAME"}
			}
			if err := st.Apps.Create(context.Background(), app); err != nil {
				t.Fatal(err)
			}
			effects := 0
			d := &AppDeployer{EnvResolver: resolver, SourceCheckout: func(context.Context, *model.App, string, io.Writer) (string, error) {
				effects++
				return "", errors.New("unexpected-source-effect")
			}}
			q := &AppDeployQueue{Store: st, Jobs: jobqueue.NewMemory(), Deployer: d, RequireSecretPins: true}
			receipt, err := q.EnqueueIntent(context.Background(), app, strings.Repeat("a", 40), &model.PipelineRun{StartedByUserSub: "accepted-actor"}, "environment-rejection")
			if err != nil {
				t.Fatal(err)
			}
			reads, latest, lists := values.pinnedReads, values.latestReads, values.lists
			env.PlainVars["DB_NAME"] = "changed-name"
			if err := st.Environments.Update(context.Background(), env); err != nil {
				t.Fatal(err)
			}
			job := claimAppPreflightTest(t, q)
			ctx := appPreflightTestContext(q, job)
			if err := q.Handle(ctx, job); err != nil {
				t.Fatal(err)
			}
			run, _ := st.Runs.Get(context.Background(), receipt.RunID)
			rows, _ := st.AppDeploys.ListByApp(context.Background(), app.ID, 100)
			if run.Status != model.RunStatusFailed || run.StartedAt != nil || len(run.StageRuns) != 0 || len(rows) != 1 || rows[0].Kind != model.AppDeployKindRejected || rows[0].Status != model.RunStatusFailed || rows[0].ImageRef != "" || rows[0].Digest != "" {
				t.Fatal("environment rejection lost its effect-free history")
			}
			before, row := run.Clone(), *rows[0]
			for range 3 {
				if err := q.Handle(ctx, job); err != nil {
					t.Fatal("retained rejection did not reconcile", err)
				}
			}
			if err := q.Jobs.(jobqueue.LeaseStore).Suspend(context.Background(), job.LockedBy, job.ID, "completion unavailable"); err != nil {
				t.Fatal(err)
			}
			attention, _ := q.Jobs.Get(context.Background(), job.ID)
			if err := RecoverJobRun(st)(context.Background(), attention); err != nil {
				t.Fatal("recovery lost environment rejection", err)
			}
			after, _ := st.Runs.Get(context.Background(), receipt.RunID)
			rows, _ = st.AppDeploys.ListByApp(context.Background(), app.ID, 100)
			if !reflect.DeepEqual(before, after) || len(rows) != 1 || !reflect.DeepEqual(row, *rows[0]) || effects != 0 || values.pinnedReads != reads || values.latestReads != latest || values.lists != lists {
				t.Fatal("rejection retry/recovery changed evidence or reached execution/credentials")
			}
		})
	}
}

func TestAppEnvironmentPreflightTransientReadAndHistoryFailure(t *testing.T) {
	for _, mode := range []string{"transient-read", "history-unavailable", "stale-claim"} {
		t.Run(mode, func(t *testing.T) {
			st, resolver, values, env := immutableSecretFixture(t)
			app := dockerfileScopedApp(env.ID)
			if err := st.Apps.Create(context.Background(), app); err != nil {
				t.Fatal(err)
			}
			q := &AppDeployQueue{Store: st, Jobs: jobqueue.NewMemory(), Deployer: &AppDeployer{EnvResolver: resolver}, RequireSecretPins: true}
			receipt, err := q.Enqueue(context.Background(), app, strings.Repeat("a", 40), nil)
			if err != nil {
				t.Fatal(err)
			}
			history := st.AppDeploys
			job := claimAppPreflightTest(t, q)
			ctx := appPreflightTestContext(q, job)
			switch mode {
			case "transient-read":
				st.Environments = unavailablePreflightEnvironment{st.Environments}
			case "history-unavailable", "stale-claim":
				env.PlainVars["DB_NAME"] = "changed-name"
				if err := st.Environments.Update(context.Background(), env); err != nil {
					t.Fatal(err)
				}
				if mode == "history-unavailable" {
					st.AppDeploys = unavailablePreflightHistory{history}
				} else if err := q.Jobs.(jobqueue.LeaseStore).Suspend(context.Background(), job.LockedBy, job.ID, "claim revoked"); err != nil {
					t.Fatal(err)
				}
			}
			if err := q.Handle(ctx, job); !errors.Is(err, jobqueue.ErrOutcomeUnknown) || strings.Contains(err.Error(), "canary") {
				t.Fatal("unavailable/stale outcome fabricated a rejection or leaked", err)
			}
			run, _ := st.Runs.Get(context.Background(), receipt.RunID)
			rows, _ := history.ListByApp(context.Background(), app.ID, 100)
			if run.Status != model.RunStatusPending || run.StartedAt != nil || run.FinishedAt != nil || len(rows) != 0 || values.pinnedReads != 0 || values.latestReads != 0 {
				t.Fatal("uncertain rejection changed run/history or read credentials")
			}
		})
	}
}
