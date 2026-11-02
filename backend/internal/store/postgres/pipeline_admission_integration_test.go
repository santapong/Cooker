//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestPipelineAdmissionLifecycleRecoversAbsentView(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending cancellation", true: "expired claim recovery"}[claimed], func(t *testing.T) {
			db := claimTestDB(t)
			ctx := context.Background()
			pipelines := NewPipelineStore(db)
			runs := NewRunStore(db)
			p := &model.Pipeline{ID: "p", Name: "accepted pipeline"}
			if err := pipelines.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
			run := &model.PipelineRun{ID: "r", PipelineID: p.ID, PipelineVersion: p.Version, Status: model.RunStatusPending, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), StartedByUserSub: "actor", StartedByGroups: []string{"deployers"}, Variables: map[string]string{"CONFIG": "plain"}, StageRuns: []model.StageRun{{StageID: "build", Status: model.RunStatusPending}}}
			payload, err := json.Marshal(map[string]any{"pipelineId": p.ID, "runId": run.ID, "pipeline": p, "run": run})
			if err != nil {
				t.Fatal(err)
			}
			jobs := jobqueue.NewPostgres(db)
			jobs.OnTransition = SyncJobRun
			job, err := jobs.Enqueue(ctx, "pipeline-run", payload, jobqueue.EnqueueOptions{ID: "run:r", MaxAttempts: 1})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runs.Get(ctx, run.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("view created early: %v", err)
			}
			want := model.RunStatusCancelled
			if claimed {
				if _, err := jobs.Dequeue(ctx, "worker", []string{"pipeline-run"}); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`UPDATE jobs SET locked_at=NOW()-INTERVAL '2 minutes' WHERE id=$1`, job.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := jobs.RecoverExpired(ctx, time.Now().Add(-time.Minute)); err != nil {
					t.Fatal(err)
				}
				want = model.RunStatusNeedsAttention
			} else {
				if _, err := jobs.Cancel(ctx, job.ID); err != nil {
					t.Fatal(err)
				}
			}
			got, err := runs.Get(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != want || got.StartedByUserSub != "actor" || got.StartedByGroups[0] != "deployers" || got.Variables["CONFIG"] != "plain" || len(got.StageRuns) != 1 || got.PipelineVersion != p.Version || !got.CreatedAt.Equal(run.CreatedAt) {
				t.Fatalf("recovery lost accepted snapshot: %+v", got)
			}
		})
	}
}

func TestPipelineAdmissionDeletedPipelineDoesNotWedgeRecovery(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	jobs := jobqueue.NewPostgres(db)
	jobs.OnTransition = SyncJobRun
	run := &model.PipelineRun{ID: "r", PipelineID: "deleted", Status: model.RunStatusPending, CreatedAt: time.Now().UTC()}
	payload, _ := json.Marshal(map[string]any{"pipelineId": run.PipelineID, "runId": run.ID, "run": run})
	if _, err := jobs.Enqueue(ctx, "pipeline-run", payload, jobqueue.EnqueueOptions{ID: "run:r"}); err != nil {
		t.Fatal(err)
	}
	if status, err := jobs.Cancel(ctx, "run:r"); err != nil || status != jobqueue.StatusCancelled {
		t.Fatalf("cancel deleted view: %s %v", status, err)
	}
	if _, err := NewPipelineStore(db).Get(ctx, "deleted"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("resurrected deleted definition: %v", err)
	}
}
