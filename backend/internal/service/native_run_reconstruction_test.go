package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

// This factory uses the actual producer and claim. Source-backed preparation is
// the existing local unit fixture, never a provider request or runtime build.
func reconstructionHeartbeatFixture(t *testing.T, sourceBound bool) (*store.Store, *jobqueue.Memory, *jobqueue.Job, context.Context, func() [3]int) {
	t.Helper()
	ctx := context.Background()
	var st *store.Store
	var p *model.Pipeline
	var preparer *PipelineSourcePreparer
	var fixture *nativeSourceFixture
	if sourceBound {
		preparer, p, fixture = nativeSourceSetup(t)
		st = preparer.Store
	} else {
		st = memory.New()
		p = &model.Pipeline{ID: "heartbeat-source-free", Name: "Source-free native", Version: 1, Stages: []model.Stage{{ID: "first", Type: model.StageTypeCustom}, {ID: "second", Type: model.StageTypeCustom}}}
		p.SetDefaults()
		if err := st.Pipelines.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	jobs := jobqueue.NewMemory()
	producer := NewJobQueueEnqueuer(jobs, st.Pipelines)
	producer.AdmissionGuard = memory.NewTriggerStore()
	producer.RequireRegistryPins = true
	producer.PrepareRegistry = func(_ context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		_, bindings, err := PipelineRegistrySelection(p)
		if err != nil {
			return nil, err
		}
		return &model.RegistryAuthorityPlan{Version: 1, Subject: model.RegistrySubject{Kind: "pipeline", ID: p.ID}, Connections: map[string]model.RegistryConnectionPin{}, Stages: bindings}, nil
	}
	if sourceBound {
		producer.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
			return preparer.Prepare(ctx, p, p.Source.FullRef, "manual")
		}
	}
	run := pendingPipelineRun(p, "native-heartbeat-default-control")
	if err := producer.EnqueuePlannedRun(ctx, p, run); err != nil {
		t.Fatal("actual reconstruction fixture admission failed", err)
	}
	job, err := jobs.Dequeue(ctx, "heartbeat-default-owner", []string{JobKindPipelineRun})
	if err != nil || job == nil || job.Attempts != 1 {
		t.Fatal("actual reconstruction fixture claim failed", err)
	}
	claim := store.WithRunClaim(ctx, job.ID, job.LockedBy, func(ctx context.Context, write func() error) error {
		return jobs.WithClaim(ctx, job.LockedBy, job.ID, write)
	})
	counts := func() [3]int {
		if fixture == nil {
			return [3]int{}
		}
		return [3]int{fixture.identities, fixture.revisions, fixture.checkouts}
	}
	return st, jobs, job, claim, counts
}

func TestReconstructibleNativeHeartbeatModernMissingViews(t *testing.T) {
	for _, bound := range []bool{false, true} {
		name := "source-free"
		if bound {
			name = "source-bound"
		}
		t.Run(name, func(t *testing.T) {
			st, _, job, claim, counts := reconstructionHeartbeatFixture(t, bound)
			before := counts()
			if !reconstructibleNativeRun(job) {
				t.Fatal("actual accepted pending view was not reconstructible")
			}
			if err := HeartbeatJobRun(st)(claim, job); err != nil {
				t.Fatal("modern missing-view creation race was cancelled", err)
			}
			if _, err := st.Runs.Get(context.Background(), jobRunID(job)); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("heartbeat created a run instead of merely tolerating its view race")
			}
			if counts() != before {
				t.Fatal("heartbeat performed a new source operation")
			}
		})
	}
}

func TestReconstructibleNativeHeartbeatRejectsAuthorityAndEffects(t *testing.T) {
	st, _, job, claim, counts := reconstructionHeartbeatFixture(t, false)
	before := counts()
	cases := []struct {
		name string
		edit func(*runJobPayload)
	}{
		{"registry-version", func(p *runJobPayload) { p.Registry.Version = 2 }},
		{"registry-subject", func(p *runJobPayload) { p.Registry.Subject.ID = "foreign-pipeline" }},
		{"registry-missing-bindings", func(p *runJobPayload) { delete(p.Registry.Stages, "first") }},
		{"registry-extra-bindings", func(p *runJobPayload) { p.Registry.Stages["foreign"] = []string{} }},
		{"registry-missing-connections-map", func(p *runJobPayload) { p.Registry.Connections = nil }},
		{"registry-selection-drift", func(p *runJobPayload) { p.Pipeline.Stages[0].Config.RegistryConnectionIDs = []string{"ungranted"} }},
		{"duplicate-authored-stage", func(p *runJobPayload) { p.Pipeline.Stages[1].ID = p.Pipeline.Stages[0].ID }},
		{"duplicate-run-stage", func(p *runJobPayload) { p.Run.StageRuns[1].StageID = p.Run.StageRuns[0].StageID }},
		{"stage-artifact", func(p *runJobPayload) {
			p.Run.StageRuns[0].Artifacts = []model.Artifact{{Type: "oci-image", Ref: "prior-artifact"}}
		}},
		{"stage-finished", func(p *runJobPayload) { at := time.Now().UTC(); p.Run.StageRuns[0].FinishedAt = &at }},
		{"stage-error", func(p *runJobPayload) { p.Run.StageRuns[0].Error = "prior-stage-error" }},
		{"environment-status", func(p *runJobPayload) {
			p.Run.EnvironmentStatuses = []model.EnvironmentStatus{{EnvironmentID: "prior", Status: model.EnvStatusDeployed}}
		}},
		{"run-heartbeat", func(p *runJobPayload) { at := time.Now().UTC(); p.Run.HeartbeatAt = &at }},
		{"review-only-stage", func(p *runJobPayload) { p.Pipeline.Stages[0].Config.ReviewOnly = true }},
		{"source-free-borrowed-plan", func(p *runJobPayload) { p.Source = &model.PipelineSourcePlan{Version: 1} }},
		{"source-free-borrowed-trigger", func(p *runJobPayload) {
			p.Trigger = &model.PipelineTriggerProof{TargetKind: "pipeline", TargetID: p.PipelineID}
		}},
		{"source-free-borrowed-receipt", func(p *runJobPayload) { p.Run.Source = &model.PipelineRunSource{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := *job
			var payload runJobPayload
			if json.Unmarshal(job.Payload, &payload) != nil {
				t.Fatal("accepted fixture cannot be copied")
			}
			tc.edit(&payload)
			var err error
			bad.Payload, err = json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if reconstructibleNativeRun(&bad) {
				t.Fatal("contradictory pending view was accepted")
			}
			if err := HeartbeatJobRun(st)(claim, &bad); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("missing-view error was suppressed", err)
			}
		})
	}
	if counts() != before {
		t.Fatal("negative heartbeat checks performed source operations")
	}
}

type reconstructionHeartbeatErrorStore struct {
	store.RunStore
	err error
}

func (s *reconstructionHeartbeatErrorStore) UpdateHeartbeat(context.Context, string, time.Time) error {
	return s.err
}

func TestReconstructibleNativeHeartbeatPreservesErrorsAndClaims(t *testing.T) {
	t.Run("cancelled-context-not-found", func(t *testing.T) {
		st, _, job, _, _ := reconstructionHeartbeatFixture(t, false)
		copyStore := *st
		copyStore.Runs = &reconstructionHeartbeatErrorStore{RunStore: st.Runs, err: store.ErrNotFound}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := HeartbeatJobRun(&copyStore)(ctx, job); err != store.ErrNotFound {
			t.Fatal("cancelled missing-view error was suppressed or changed", err)
		}
	})
	for _, err := range []error{store.ErrConflict, context.DeadlineExceeded, errors.New("owned heartbeat persistence failure")} {
		t.Run(err.Error(), func(t *testing.T) {
			st, _, job, _, _ := reconstructionHeartbeatFixture(t, false)
			copyStore := *st
			copyStore.Runs = &reconstructionHeartbeatErrorStore{RunStore: st.Runs, err: err}
			if got := HeartbeatJobRun(&copyStore)(context.Background(), job); got != err {
				t.Fatal("store error was suppressed or rewritten", got)
			}
		})
	}
	t.Run("actual-stale-claim", func(t *testing.T) {
		st, jobs, job, claim, _ := reconstructionHeartbeatFixture(t, false)
		if _, err := jobs.Cancel(context.Background(), job.ID); err != nil {
			t.Fatal(err)
		}
		if err := HeartbeatJobRun(st)(claim, job); !errors.Is(err, jobqueue.ErrNotLocked) {
			t.Fatal("stale queue ownership was converted to view tolerance", err)
		}
	})
}
