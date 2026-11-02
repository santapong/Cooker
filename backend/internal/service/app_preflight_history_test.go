package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"reflect"
	"strings"
	"testing"
)

type unavailablePreflightHistory struct{ store.AppDeployStore }

func (unavailablePreflightHistory) RecordAppPreflightFailure(context.Context, store.AppPreflightFailure) error {
	return errors.New("synthetic-outcome-storage-canary")
}

type unavailablePreflightApp struct{ store.AppStore }

func (unavailablePreflightApp) Get(context.Context, string) (*model.App, error) {
	return nil, errors.New("synthetic-app-read-canary")
}
func claimAppPreflightTest(t *testing.T, q *AppDeployQueue) *jobqueue.Job {
	t.Helper()
	j, err := q.Jobs.Dequeue(context.Background(), "preflight-worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func appPreflightTestContext(q *AppDeployQueue, j *jobqueue.Job) context.Context {
	return store.WithRunClaim(context.Background(), j.ID, j.LockedBy, func(c context.Context, write func() error) error {
		return q.Jobs.(*jobqueue.Memory).WithClaim(c, j.LockedBy, j.ID, write)
	})
}
func TestAppPreflightHistoryPersistenceFailureIsSafeUncertain(t *testing.T) {
	q, app, values, config := appRegistryQueueFixture(t)
	receipt, err := q.Enqueue(context.Background(), app, strings.Repeat("a", 40), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Store.Registries.(store.RegistryGrantStore).UpdateGrants(context.Background(), config.ID, config.Version, nil, nil); err != nil {
		t.Fatal(err)
	}
	q.Store.AppDeploys = unavailablePreflightHistory{q.Store.AppDeploys}
	j := claimAppPreflightTest(t, q)
	err = q.Handle(appPreflightTestContext(q, j), j)
	if !errors.Is(err, jobqueue.ErrOutcomeUnknown) || strings.Contains(err.Error(), "canary") {
		t.Fatal("storage failure completed or leaked")
	}
	run, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
	if run.Status != model.RunStatusPending || run.FinishedAt != nil {
		t.Fatal("failed atomic outcome changed run")
	}
	if values.reads != 0 || values.latestReads != 0 {
		t.Fatal("rejection read secrets")
	}
}
func TestAppPreflightHistoryRetryAndRecoveryPreserveDecision(t *testing.T) {
	q, app, values, config := appRegistryQueueFixture(t)
	receipt, err := q.EnqueueIntent(context.Background(), app, strings.Repeat("a", 40), &model.PipelineRun{StartedByUserSub: "accepted"}, "decision-intent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Store.Registries.(store.RegistryGrantStore).UpdateGrants(context.Background(), config.ID, config.Version, nil, nil); err != nil {
		t.Fatal(err)
	}
	j := claimAppPreflightTest(t, q)
	ctx := appPreflightTestContext(q, j)
	if err := q.Handle(ctx, j); err != nil {
		t.Fatal(err)
	}
	before, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
	rows, _ := q.Store.AppDeploys.ListByApp(context.Background(), app.ID, 100)
	if len(rows) != 1 || rows[0].Kind != model.AppDeployKindRejected {
		t.Fatal("rejection missing")
	}
	old := *rows[0]
	q.RegistryAuthority = nil
	q.RequireRegistryPins = true
	for i := 0; i < 6; i++ {
		if err := q.Handle(ctx, j); err != nil {
			t.Fatal("retained decision retried execution")
		}
	}
	if err := q.Jobs.(jobqueue.LeaseStore).Suspend(context.Background(), j.LockedBy, j.ID, "lost after decision"); err != nil {
		t.Fatal(err)
	}
	attention, _ := q.Jobs.Get(context.Background(), j.ID)
	if err := RecoverJobRun(q.Store)(context.Background(), attention); err != nil {
		t.Fatal(err)
	}
	after, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
	rows, _ = q.Store.AppDeploys.ListByApp(context.Background(), app.ID, 100)
	if !reflect.DeepEqual(before, after) || len(rows) != 1 || !reflect.DeepEqual(old, *rows[0]) || attention.Status != jobqueue.StatusNeedsAttention {
		t.Fatal("memory recovery overwrote determinate rejection")
	}
	if values.reads != 0 || values.latestReads != 0 || values.pins != 1 {
		t.Fatal("retry resolved or repinned credentials")
	}
}
func TestAppPreflightTransientAppReadRemainsUncertain(t *testing.T) {
	q, app, _, _ := appRegistryQueueFixture(t)
	receipt, err := q.Enqueue(context.Background(), app, strings.Repeat("a", 40), nil)
	if err != nil {
		t.Fatal(err)
	}
	q.Store.Apps = unavailablePreflightApp{q.Store.Apps}
	j := claimAppPreflightTest(t, q)
	err = q.Handle(appPreflightTestContext(q, j), j)
	if !errors.Is(err, jobqueue.ErrOutcomeUnknown) || strings.Contains(err.Error(), "canary") {
		t.Fatal("transient app read fabricated deleted failure")
	}
	run, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
	rows, _ := q.Store.AppDeploys.ListByApp(context.Background(), app.ID, 100)
	if run.Status != model.RunStatusPending || len(rows) != 0 {
		t.Fatal("transient read committed failure")
	}
}
func TestAppPreflightRecoveryCannotStripVersionedRejection(t *testing.T) {
	for _, mode := range []string{"invalid-commit", "unsupported-version", "zero-version", "null-version", "string-version", "missing-version"} {
		t.Run(mode, func(t *testing.T) {
			q, app, _, config := appRegistryQueueFixture(t)
			receipt, err := q.Enqueue(context.Background(), app, strings.Repeat("a", 40), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := q.Store.Registries.(store.RegistryGrantStore).UpdateGrants(context.Background(), config.ID, config.Version, nil, nil); err != nil {
				t.Fatal(err)
			}
			j := claimAppPreflightTest(t, q)
			if err := q.Handle(appPreflightTestContext(q, j), j); err != nil {
				t.Fatal(err)
			}
			before, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
			old, _ := q.Store.AppDeploys.ListByApp(context.Background(), app.ID, 100)
			var raw map[string]any
			if json.Unmarshal(j.Payload, &raw) != nil {
				t.Fatal("fixture plan unavailable")
			}
			switch mode {
			case "invalid-commit":
				raw["commit"] = "invalid"
			case "unsupported-version":
				raw["version"] = 2
			case "zero-version":
				raw["version"] = 0
			case "null-version":
				raw["version"] = nil
			case "string-version":
				raw["version"] = "1"
			case "missing-version":
				delete(raw, "version")
			}
			j.Payload, _ = json.Marshal(raw)
			j.Status = jobqueue.StatusNeedsAttention
			if err := RecoverJobRun(q.Store)(context.Background(), j); err == nil {
				t.Fatal("version stripping overwrote typed rejection")
			}
			after, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
			now, _ := q.Store.AppDeploys.ListByApp(context.Background(), app.ID, 100)
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(old, now) {
				t.Fatal("denied recovery changed decision")
			}
		})
	}
}

// Opaque wrappers retain legacy history reads but cannot silently erase the
// capabilities needed to distinguish rejected business outcomes in recovery.
type opaquePreflightHistory struct{ store.AppDeployStore }

func TestAppPreflightRecoveryMissingCapabilitiesFailsClosed(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "versioned"
		if legacy {
			name = "unversioned"
		}
		t.Run(name, func(t *testing.T) {
			q, app, _, config := appRegistryQueueFixture(t)
			receipt, err := q.Enqueue(context.Background(), app, strings.Repeat("a", 40), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := q.Store.Registries.(store.RegistryGrantStore).UpdateGrants(context.Background(), config.ID, config.Version, nil, nil); err != nil {
				t.Fatal(err)
			}
			j := claimAppPreflightTest(t, q)
			if err := q.Handle(appPreflightTestContext(q, j), j); err != nil {
				t.Fatal(err)
			}
			before, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
			history := q.Store.AppDeploys
			old, _ := history.ListByApp(context.Background(), app.ID, 100)
			if legacy {
				var raw map[string]any
				_ = json.Unmarshal(j.Payload, &raw)
				delete(raw, "version")
				j.Payload, _ = json.Marshal(raw)
			}
			q.Store.AppDeploys = opaquePreflightHistory{history}
			j.Status = jobqueue.StatusNeedsAttention
			if err := RecoverJobRun(q.Store)(context.Background(), j); err == nil {
				t.Fatal("hidden recovery capability overwrote typed rejection")
			}
			after, _ := q.Store.Runs.Get(context.Background(), receipt.RunID)
			rows, _ := history.ListByApp(context.Background(), app.ID, 100)
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(old, rows) {
				t.Fatal("missing capability changed durable rejection")
			}
		})
	}
}
