package memory

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"strings"
	"sync"
	"testing"
	"time"
)

func memoryPreflightFixture(t *testing.T) (*store.Store, *jobqueue.Memory, context.Context, store.AppPreflightFailure) {
	t.Helper()
	ctx := context.Background()
	st := New()
	jobs := jobqueue.NewMemory()
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	actor := &model.PipelineRun{StartedByUserSub: "accepted", StartedByGroups: []string{"operator"}}
	app := &model.App{ID: "app", Name: "fixture"}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"version": 1, "app": app, "runId": "run", "commit": strings.Repeat("a", 40), "createdAt": stamp, "actor": actor})
	f, err := store.DecodeAppPreflightFailure(payload)
	if err != nil {
		t.Fatal(err)
	}
	run := actor.Clone()
	run.ID, run.PipelineID, run.Status, run.CreatedAt = f.RunID, f.PipelineID, model.RunStatusPending, stamp
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if _, err = jobs.Enqueue(ctx, "app-deploy", payload, jobqueue.EnqueueOptions{ID: "app-run:run", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Dequeue(ctx, "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	claim := store.WithRunClaim(ctx, job.ID, job.LockedBy, func(ctx context.Context, write func() error) error {
		return jobs.WithClaim(ctx, job.LockedBy, job.ID, write)
	})
	claim = store.WithRunPayloadClaim(claim, func(ctx context.Context, kind, digest string, write func() error) error {
		return jobs.WithClaimPayload(ctx, job.LockedBy, job.ID, kind, digest, write)
	})
	return st, jobs, claim, f
}
func TestMemoryAppPreflightRejectsMutatedAcceptedCommit(t *testing.T) {
	st, _, claim, f := memoryPreflightFixture(t)
	f.CommitSHA = strings.Repeat("b", 40)
	if err := st.AppDeploys.(store.AppPreflightFailureRecorder).RecordAppPreflightFailure(claim, f); err == nil {
		t.Fatal("mutated commit bypassed authoritative memory payload identity")
	}
	r, _ := st.Runs.Get(context.Background(), f.RunID)
	d, _ := st.AppDeploys.ListByApp(context.Background(), f.AppID, 100)
	if r.Status != model.RunStatusPending || len(d) != 0 {
		t.Fatal("denied acceptance modified outcome")
	}
}
func TestMemoryAppPreflightAtomicRejectedAndIdempotent(t *testing.T) {
	st, _, claim, f := memoryPreflightFixture(t)
	rec := st.AppDeploys.(store.AppPreflightFailureRecorder)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- rec.RecordAppPreflightFailure(claim, f) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	r, _ := st.Runs.Get(context.Background(), f.RunID)
	rows, _ := st.AppDeploys.ListByApp(context.Background(), f.AppID, 100)
	if len(rows) != 1 || !store.AppPreflightMarkerValid(f, r, rows[0]) {
		t.Fatal("not one exact rejected decision")
	}
	stamp := *r.FinishedAt
	rows[0].Status = model.RunStatusSuccess
	rows[0].ImageRef = "forged"
	if err := rec.RecordAppPreflightFailure(claim, f); err != nil {
		t.Fatal(err)
	}
	got, _ := st.AppDeploys.Get(context.Background(), store.AppPreflightHistoryID(f.RunID))
	r, _ = st.Runs.Get(context.Background(), f.RunID)
	if !store.AppPreflightMarkerValid(f, r, got) || !r.FinishedAt.Equal(stamp) {
		t.Fatal("retry or pointer mutation changed durable outcome")
	}
}
func TestMemoryAppPreflightFencesInvalidStates(t *testing.T) {
	for _, name := range []string{"missing-claim", "stale-claim", "success", "attention", "cancelled", "started", "stage-running", "stage-finished", "stage-logs", "stage-outputs", "stage-artifact", "stage-error", "env-effects", "other-failure", "deleted-app", "history-collision", "history-other-id", "legacy"} {
		t.Run(name, func(t *testing.T) {
			st, jobs, claim, f := memoryPreflightFixture(t)
			ctx := context.Background()
			r, _ := st.Runs.Get(ctx, f.RunID)
			stamp := time.Now().UTC().Truncate(time.Microsecond)
			switch name {
			case "missing-claim":
				claim = ctx
			case "stale-claim":
				if err := jobs.Suspend(ctx, "worker", "app-run:run", "lost"); err != nil {
					t.Fatal(err)
				}
			case "success":
				r.Status = model.RunStatusSuccess
			case "attention":
				r.Status = model.RunStatusNeedsAttention
			case "cancelled":
				r.Status = model.RunStatusCancelled
			case "started":
				r.StartedAt = &stamp
			case "stage-running":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusRunning}}
			case "stage-finished":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusPending, FinishedAt: &stamp}}
			case "stage-logs":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusPending, Logs: "effect"}}
			case "stage-outputs":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusPending, Outputs: map[string]string{"effect": "yes"}}}
			case "stage-artifact":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusPending, Artifacts: []model.Artifact{{Type: "image"}}}}
			case "stage-error":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusPending, Error: "stage-failed"}}
			case "env-effects":
				r.EnvironmentStatuses = []model.EnvironmentStatus{{Status: model.EnvStatusDeploying}}
			case "other-failure":
				r.Status, r.Error, r.FinishedAt = model.RunStatusFailed, "some-other-failure", &stamp
			case "deleted-app":
				if err := st.Apps.Delete(ctx, f.AppID); err != nil {
					t.Fatal(err)
				}
			case "history-collision", "history-other-id":
				id := store.AppPreflightHistoryID(f.RunID)
				if name == "history-other-id" {
					id = "other"
				}
				if err := st.AppDeploys.Create(ctx, &model.AppDeploy{ID: id, AppID: f.AppID, RunID: f.RunID, Status: model.RunStatusFailed, Kind: model.AppDeployKindDeploy}); err != nil {
					t.Fatal(err)
				}
			case "legacy":
				r.Status, r.Error, r.FinishedAt = model.RunStatusFailed, store.AppPreflightRegistrySummary, &stamp
			}
			if err := st.Runs.Update(ctx, r); err != nil {
				t.Fatal(err)
			}
			err := st.AppDeploys.(store.AppPreflightFailureRecorder).RecordAppPreflightFailure(claim, f)
			if name == "legacy" {
				if err != nil {
					t.Fatal(err)
				}
				got, _ := st.Runs.Get(ctx, f.RunID)
				if !got.FinishedAt.Equal(stamp) {
					t.Fatal("legacy time overwritten")
				}
				return
			}
			if err == nil {
				t.Fatal("invalid state admitted")
			}
			got, _ := st.Runs.Get(ctx, f.RunID)
			if got.Status != r.Status || got.Error != r.Error {
				t.Fatal("denied state changed")
			}
			if name == "missing-claim" && !errors.Is(err, store.ErrConflict) {
				t.Fatal(err)
			}
		})
	}
}
func TestMemoryAppPreflightRejectsForgedPayloadAndWrongJob(t *testing.T) {
	for _, mode := range []string{"forged-payload", "wrong-kind", "wrong-claim-job", "missing-payload-guard"} {
		t.Run(mode, func(t *testing.T) {
			st, jobs, claim, f := memoryPreflightFixture(t)
			switch mode {
			case "forged-payload":
				var raw map[string]any
				_ = json.Unmarshal(f.AcceptedPayload, &raw)
				raw["commit"] = strings.Repeat("b", 40)
				b, _ := json.Marshal(raw)
				f, _ = store.DecodeAppPreflightFailure(b)
			case "wrong-kind":
				jobs = jobqueue.NewMemory()
				if _, err := jobs.Enqueue(context.Background(), "pipeline-run", f.AcceptedPayload, jobqueue.EnqueueOptions{ID: "app-run:run"}); err != nil {
					t.Fatal(err)
				}
				j, e := jobs.Dequeue(context.Background(), "worker", nil)
				if e != nil {
					t.Fatal(e)
				}
				claim = store.WithRunClaim(context.Background(), j.ID, j.LockedBy, nil)
				claim = store.WithRunPayloadClaim(claim, func(c context.Context, kind, digest string, w func() error) error {
					return jobs.WithClaimPayload(c, j.LockedBy, j.ID, kind, digest, w)
				})
			case "wrong-claim-job":
				claim = store.WithRunClaim(context.Background(), "app-run:other", "worker", nil)
				claim = store.WithRunPayloadClaim(claim, func(c context.Context, kind, digest string, w func() error) error {
					return jobs.WithClaimPayload(c, "worker", "app-run:run", kind, digest, w)
				})
			case "missing-payload-guard":
				claim = store.WithRunClaim(context.Background(), "app-run:run", "worker", func(c context.Context, w func() error) error { return jobs.WithClaim(c, "worker", "app-run:run", w) })
			}
			if err := st.AppDeploys.(store.AppPreflightFailureRecorder).RecordAppPreflightFailure(claim, f); err == nil {
				t.Fatal("unbound payload authority admitted")
			}
			r, _ := st.Runs.Get(context.Background(), "run")
			d, _ := st.AppDeploys.ListByApp(context.Background(), "app", 100)
			if r.Status != model.RunStatusPending || len(d) != 0 {
				t.Fatal("forged payload changed state")
			}
		})
	}
}
func TestMemoryAppPreflightRecoveryRejectsContradictoryEvidence(t *testing.T) {
	for _, mode := range []string{"email", "groups", "token", "time", "source", "foreign-history", "typed-artifact", "typed-started", "typed-other-row"} {
		t.Run(mode, func(t *testing.T) {
			st, _, claim, f := memoryPreflightFixture(t)
			ctx := context.Background()
			if err := st.AppDeploys.(store.AppPreflightFailureRecorder).RecordAppPreflightFailure(claim, f); err != nil {
				t.Fatal(err)
			}
			r := st.Runs.(*runs)
			d := st.AppDeploys.(*appDeploys)
			r.mu.Lock()
			switch mode {
			case "email":
				r.m[f.RunID].StartedByEmail = "other"
			case "groups":
				r.m[f.RunID].StartedByGroups = []string{"other"}
			case "token":
				r.m[f.RunID].StartedByTokenHash = "other"
			case "time":
				r.m[f.RunID].CreatedAt = r.m[f.RunID].CreatedAt.Add(time.Second)
			case "source":
				r.m[f.RunID].Source = &model.PipelineRunSource{}
			case "typed-started":
				stamp := time.Now()
				r.m[f.RunID].StartedAt = &stamp
			}
			r.mu.Unlock()
			d.mu.Lock()
			switch mode {
			case "foreign-history":
				d.m[store.AppPreflightHistoryID(f.RunID)].AppID = "other"
			case "typed-artifact":
				d.m[store.AppPreflightHistoryID(f.RunID)].ImageRef = "possible-effect"
			case "typed-other-row":
				d.m["other"] = &model.AppDeploy{ID: "other", AppID: f.AppID, RunID: f.RunID, PipelineID: f.PipelineID, Kind: model.AppDeployKindDeploy}
			}
			d.mu.Unlock()
			if preserved, err := st.AppDeploys.(store.AppPreflightFailureReader).HasAppPreflightFailure(ctx, f); err == nil || preserved {
				t.Fatal("contradictory evidence recovered or hidden")
			}
		})
	}
}
