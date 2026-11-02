package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestPipelineAdmissionRecoversBeforeRunViewAndPinsActor(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	jobs := jobqueue.NewMemory()
	p, run := simpleBuildPushPipeline("admitted", "accepted")
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	run.PipelineVersion = p.Version
	run.StartedByUserSub, run.StartedByEmail = "actor-sub", "actor@example.test"
	run.StartedByGroups, run.StartedByTokenHash = []string{"operators"}, "hash-only"
	e := NewJobQueueEnqueuer(jobs, st.Pipelines)
	if err := e.EnqueuePlannedRun(ctx, p, run); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Runs.Get(ctx, run.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("run created before worker: %v", err)
	}
	// Simulate an admission-process crash, followed by a definition edit.
	p.Stages = []model.Stage{{ID: "changed", Type: "unsupported"}}
	p.Version++
	job, err := jobs.Dequeue(ctx, "worker", []string{JobKindPipelineRun})
	if err != nil || job == nil {
		t.Fatalf("dequeue: %v", err)
	}
	owned := store.WithRunClaim(ctx, job.ID, "worker", func(ctx context.Context, write func() error) error {
		return jobs.WithClaim(ctx, "worker", job.ID, write)
	})
	if err := NewJobQueueRunner(st, NewExecutor(), nil).Handle(owned, job); err != nil {
		t.Fatal(err)
	}
	got, err := st.Runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.RunStatusSuccess || got.StartedByUserSub != "actor-sub" || got.StartedByEmail != "actor@example.test" || got.StartedByTokenHash != "hash-only" || len(got.StartedByGroups) != 1 || got.StartedByGroups[0] != "operators" || len(got.StageRuns) != 2 || !got.CreatedAt.Equal(run.CreatedAt) {
		t.Fatalf("accepted snapshot not preserved: %+v", got)
	}
}

func TestPipelineAdmissionDoesNotOverwriteConcurrentCancellation(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	jobs := jobqueue.NewMemory()
	p, run := simpleBuildPushPipeline("p", "r")
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	run.PipelineVersion = p.Version
	if err := NewJobQueueEnqueuer(jobs, st.Pipelines).EnqueuePlannedRun(ctx, p, run); err != nil {
		t.Fatal(err)
	}
	cancelled := run.Clone()
	cancelled.Status = model.RunStatusCancelled
	if err := st.Runs.Create(ctx, cancelled); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Dequeue(ctx, "worker", nil)
	if err != nil || job == nil {
		t.Fatalf("dequeue: %v", err)
	}
	if err := NewJobQueueRunner(st, NewExecutor(), nil).Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Runs.Get(ctx, run.ID)
	if got.Status != model.RunStatusCancelled || got.StartedAt != nil {
		t.Fatalf("cancelled view overwritten: %+v", got)
	}
}

func TestPipelineAdmissionRejectsConflictingViewActor(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	jobs := jobqueue.NewMemory()
	p, run := simpleBuildPushPipeline("p", "r")
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	run.PipelineVersion = p.Version
	run.StartedByUserSub = "approved-actor"
	if err := NewJobQueueEnqueuer(jobs, st.Pipelines).EnqueuePlannedRun(ctx, p, run); err != nil {
		t.Fatal(err)
	}
	changed := run.Clone()
	changed.StartedByUserSub = "different-actor"
	if err := st.Runs.Create(ctx, changed); err != nil {
		t.Fatal(err)
	}
	job, _ := jobs.Dequeue(ctx, "worker", nil)
	if err := NewJobQueueRunner(st, NewExecutor(), nil).Handle(ctx, job); err == nil {
		t.Fatal("accepted altered actor")
	}
	got, _ := st.Runs.Get(ctx, run.ID)
	if got.StartedAt != nil {
		t.Fatal("executed before identity check")
	}
}

func TestPipelineAdmissionScheduledRunHasReconstructableSnapshot(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	jobs := jobqueue.NewMemory()
	p, _ := simpleBuildPushPipeline("p", "r")
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := NewJobQueueEnqueuer(jobs, st.Pipelines).EnqueueRun(ctx, p.ID, "scheduled"); err != nil {
		t.Fatal(err)
	}
	job, _ := jobs.Dequeue(ctx, "worker", nil)
	var payload runJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Run == nil || payload.Run.ID != "scheduled" || payload.Run.PipelineVersion != p.Version {
		t.Fatalf("missing scheduled snapshot: %+v", payload)
	}
	if err := NewJobQueueRunner(st, NewExecutor(), nil).Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
}
