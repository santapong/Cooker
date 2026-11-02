package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

// Only owned source/secret/builder fixtures are used. A source plan is new
// accepted work and must never acquire legacy current-definition/actor fallback
// or turn reviewed/automatic authority into a manual checkout by losing proof.
func TestNativeSourceWorkerRejectsMissingProofAndCapturedSnapshotsBeforeReads(t *testing.T) {
	for _, scenario := range []string{"automatic-proof", "review-proof", "pipeline-snapshot", "run-snapshot", "both-snapshots"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			s, p, provider := nativeSourceSetup(t)
			st, resolver, manager, env := immutableSecretFixture(t)
			s.Store = st
			p.Stages = append(p.Stages, model.Stage{ID: "test", Type: model.StageTypeTest, EnvironmentID: env.ID, Config: model.StageConfig{Image: "owned-fixture", SecretRefs: []string{"TOKEN"}}})
			if err := st.Pipelines.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
			policy := "manual"
			if scenario == "automatic-proof" {
				policy = "automatic"
			} else if scenario == "review-proof" {
				policy = "review"
			}
			source, err := s.Prepare(ctx, p, p.Source.FullRef, policy)
			if err != nil {
				t.Fatal(err)
			}
			build, stage := &nativeBytesBuilder{}, &fakeStageRunner{}
			executor := NewExecutor(WithBuilder(build), WithStageRunner(stage), WithEnvironmentResolver(resolver))
			secrets, err := executor.PreparePipelineSecrets(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			run := pendingPipelineRun(p, "boundary-"+scenario)
			if err := st.Runs.Create(ctx, run); err != nil {
				t.Fatal(err)
			}
			payload := runJobPayload{PipelineID: p.ID, RunID: run.ID, Pipeline: p, Run: run, Source: source, Secrets: secrets}
			if scenario == "pipeline-snapshot" {
				payload.Pipeline = nil
			} else if scenario == "run-snapshot" {
				payload.Run = nil
			} else if scenario == "both-snapshots" {
				payload.Pipeline, payload.Run = nil, nil
			}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			job := &jobqueue.Job{ID: "run:" + run.ID, Kind: JobKindPipelineRun, ConcurrencyKey: "pipeline:" + p.ID, Payload: data}
			runner := NewJobQueueRunner(st, executor, nil)
			runner.SourcePreparer = s
			providerBefore := provider.identities + provider.revisions + provider.checkouts
			secretBefore := manager.lists + manager.latestReads + manager.pinnedReads
			err = runner.Handle(ctx, job)
			if err == nil || errors.Is(err, jobqueue.ErrOutcomeUnknown) {
				t.Error("missing captured authority did not reject determinately", err)
			}
			if provider.identities+provider.revisions+provider.checkouts != providerBefore || manager.lists+manager.latestReads+manager.pinnedReads != secretBefore || build.calls != 0 || stage.callCount() != 0 {
				t.Error("rejected source boundary reached provider/source/secret/native effects")
			}
			stored, err := st.Runs.Get(ctx, run.ID)
			if err != nil || stored.Source != nil && stored.Source.CheckedOutAt != nil {
				t.Error("rejected source boundary claimed checkout", err)
			}
		})
	}
}

func TestNativeSourceWorkerSecretPreflightFailureIsDeterminate(t *testing.T) {
	ctx := context.Background()
	s, p, _ := nativeSourceSetup(t)
	st, resolver, manager, env := immutableSecretFixture(t)
	s.Store = st
	p.Stages = append(p.Stages, model.Stage{ID: "test", Type: model.StageTypeTest, EnvironmentID: env.ID, Config: model.StageConfig{Image: "owned-fixture", SecretRefs: []string{"TOKEN"}}})
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	source, err := s.Prepare(ctx, p, p.Source.FullRef, "manual")
	if err != nil {
		t.Fatal(err)
	}
	build, stage := &nativeBytesBuilder{}, &fakeStageRunner{}
	executor := NewExecutor(WithBuilder(build), WithStageRunner(stage), WithEnvironmentResolver(resolver))
	run := pendingPipelineRun(p, "secret-preflight")
	// Source is valid; the missing immutable secret plan must fail before stages.
	payload, _ := json.Marshal(runJobPayload{PipelineID: p.ID, RunID: run.ID, Pipeline: p, Run: run, Source: source})
	job := &jobqueue.Job{ID: "run:" + run.ID, Kind: JobKindPipelineRun, ConcurrencyKey: "pipeline:" + p.ID, Payload: payload}
	runner := NewJobQueueRunner(st, executor, nil)
	runner.SourcePreparer = s
	before := manager.lists + manager.latestReads + manager.pinnedReads
	err = runner.Handle(ctx, job)
	if err == nil || errors.Is(err, jobqueue.ErrOutcomeUnknown) {
		t.Error("pre-stage secret rejection incorrectly became ambiguous", err)
	}
	stored, loadErr := st.Runs.Get(ctx, run.ID)
	if loadErr != nil || stored.Status != model.RunStatusFailed || stored.StartedAt != nil || build.calls != 0 || stage.callCount() != 0 || manager.lists+manager.latestReads+manager.pinnedReads != before {
		t.Fatal("preflight failure reached native stages or lost determinate state", loadErr)
	}
}

func TestNativeSourceWorkerPreflightDoesNotEraseEarlierEffectUncertainty(t *testing.T) {
	for _, earlier := range []string{"run", "stage"} {
		t.Run(earlier, func(t *testing.T) {
			ctx := context.Background()
			s, p, provider := nativeSourceSetup(t)
			source, err := s.Prepare(ctx, p, p.Source.FullRef, "manual")
			if err != nil {
				t.Fatal(err)
			}
			planned := pendingPipelineRun(p, "earlier-"+earlier)
			progress := planned.Clone()
			started := time.Now().UTC().Add(-time.Minute)
			if earlier == "run" {
				progress.Status = model.RunStatusRunning
				progress.StartedAt = &started
			} else {
				// An independently recorded stage start is enough even when the
				// containing lifecycle view is incomplete or still marked pending.
				progress.StageRuns[0].Status = model.RunStatusRunning
				progress.StageRuns[0].StartedAt = &started
			}
			if err := s.Store.Runs.Create(ctx, progress); err != nil {
				t.Fatal(err)
			}
			connection, err := s.Connections.Store.Get(ctx, p.Source.ConnectionID)
			if err != nil {
				t.Fatal(err)
			}
			connection.Enabled = false
			if err := s.Connections.Store.Update(ctx, connection); err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(runJobPayload{PipelineID: p.ID, RunID: planned.ID, Pipeline: p, Run: planned, Source: source})
			job := &jobqueue.Job{ID: "run:" + planned.ID, Kind: JobKindPipelineRun, ConcurrencyKey: "pipeline:" + p.ID, Payload: payload}
			build := &nativeBytesBuilder{}
			runner := NewJobQueueRunner(s.Store, NewExecutor(WithBuilder(build)), nil)
			runner.SourcePreparer = s
			before := provider.identities + provider.revisions + provider.checkouts
			if err := runner.Handle(ctx, job); !errors.Is(err, jobqueue.ErrOutcomeUnknown) {
				t.Error("later preflight rejection erased prior effect uncertainty", err)
			}
			if provider.identities+provider.revisions+provider.checkouts != before || build.calls != 0 {
				t.Fatal("prior uncertainty test made new provider/build effects")
			}
			stored, err := s.Store.Runs.Get(ctx, planned.ID)
			if err != nil || stored.Source != nil && stored.Source.CheckedOutAt != nil {
				t.Fatal("rejected reopened source claimed checkout", err)
			}
		})
	}
}
