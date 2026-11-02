package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/pusher"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

// Embedding only the existing interface deliberately hides the optional
// source-recorder capability without changing ordinary run storage behavior.
type sourcePersistenceMissingRecorder struct{ store.RunStore }

type sourcePersistenceErrorRecorder struct {
	store.RunStore
	err   error
	calls atomic.Int32
}

func (s *sourcePersistenceErrorRecorder) RecordSourceCheckout(_ context.Context, _ string, source *model.PipelineRunSource) error {
	s.calls.Add(1)
	if source == nil || source.CheckedOutAt == nil || source.Validate() != nil {
		return model.ErrInvalidRunSource
	}
	return s.err
}

type sourcePersistenceNoEffects struct{ builds, pushes, deploys atomic.Int32 }

func (s *sourcePersistenceNoEffects) Build(context.Context, builder.Request) (builder.Result, error) {
	s.builds.Add(1)
	return builder.Result{}, errors.New("unexpected boundary build")
}
func (s *sourcePersistenceNoEffects) Push(context.Context, pusher.Request) (pusher.Result, error) {
	s.pushes.Add(1)
	return pusher.Result{}, errors.New("unexpected boundary push")
}
func (s *sourcePersistenceNoEffects) Deploy(context.Context, deployer.Request) (deployer.Result, error) {
	s.deploys.Add(1)
	return deployer.Result{}, errors.New("unexpected boundary deployment")
}

func TestNativeSourcePersistenceFailureStopsStagesWithSafeSummary(t *testing.T) {
	const summary = "verified checkout persistence is unavailable"
	for _, mode := range []string{"canary-storage-error", "missing-capability", "cancelled-storage-error"} {
		t.Run(mode, func(t *testing.T) {
			// This bounds both original local checkout and exported source to
			// disposable test storage; no daemon, provider or network is used.
			private := t.TempDir()
			t.Setenv("TMPDIR", private)
			preparer, p, fixture := nativeSourceSetup(t)
			p.Stages = append(p.Stages,
				model.Stage{ID: "push", Type: model.StageTypePush},
				model.Stage{ID: "deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{DeployRuntime: "docker"}},
			)
			p.Edges = []model.Edge{{Source: "build", Target: "push"}, {Source: "push", Target: "deploy"}}
			st := preparer.Store
			ctx := context.Background()
			jobs := jobqueue.NewMemory()
			producer := NewJobQueueEnqueuer(jobs, st.Pipelines)
			producer.AdmissionGuard = memory.NewTriggerStore()
			authority := NewRegistryAuthorityService(st, nil)
			producer.RequireRegistryPins = true
			producer.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
				return PreparePipelineRegistryAuthority(ctx, authority, p)
			}
			producer.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
				return preparer.Prepare(ctx, p, p.Source.FullRef, "manual")
			}
			run := pendingPipelineRun(p, "native-source-persistence-boundary")
			if err := producer.EnqueuePlannedRun(ctx, p, run); err != nil {
				t.Fatal("actual boundary admission failed", err)
			}
			job, err := jobs.Dequeue(ctx, "source-persistence-boundary-owner", []string{JobKindPipelineRun})
			if err != nil || job == nil || job.Attempts != 1 {
				t.Fatal("actual boundary claim failed", err)
			}
			claim := store.WithRunClaim(ctx, job.ID, job.LockedBy, func(ctx context.Context, write func() error) error {
				return jobs.WithClaim(ctx, job.LockedBy, job.ID, write)
			})
			canary := "owned-recorder-error-canary-" + mode
			cause := errors.New(canary)
			if mode == "cancelled-storage-error" {
				cause = errors.Join(context.Canceled, cause)
			}
			var errorStore *sourcePersistenceErrorRecorder
			if mode == "missing-capability" {
				st.Runs = &sourcePersistenceMissingRecorder{RunStore: st.Runs}
			} else {
				errorStore = &sourcePersistenceErrorRecorder{RunStore: st.Runs, err: cause}
				st.Runs = errorStore
			}
			adapters := &sourcePersistenceNoEffects{}
			var progress atomic.Int32
			executor := NewExecutor(WithBuilder(adapters), WithPusher(adapters), WithDockerDeployer(adapters), WithRegistryAuthority(authority), WithRunUpdater(func(context.Context, *model.PipelineRun) error {
				progress.Add(1)
				return errors.New("unexpected stage progress")
			}))
			runner := NewJobQueueRunner(st, executor, nil)
			runner.SourcePreparer, runner.RegistryAuthority = preparer, authority
			checkoutsBefore := fixture.checkouts
			err = runner.Handle(claim, job)
			if err == nil || err.Error() != summary {
				t.Fatal("failed persistence did not return a fixed public summary", err)
			}
			if mode != "missing-capability" && !errors.Is(err, cause) {
				t.Fatal("fixed summary discarded the internal error cause")
			}
			if mode == "cancelled-storage-error" && !errors.Is(err, context.Canceled) {
				t.Fatal("safe wrapper discarded cancellation classification")
			}
			stored, getErr := st.Runs.Get(ctx, run.ID)
			status := model.RunStatusFailed
			if mode == "cancelled-storage-error" {
				status = model.RunStatusCancelled
			}
			if getErr != nil || stored.Status != status || stored.Error != summary || stored.StartedAt != nil || stored.FinishedAt == nil {
				t.Fatal("retained run did not preserve safe pre-stage failure", getErr)
			}
			for _, stage := range stored.StageRuns {
				if stage.Status != model.RunStatusPending || stage.StartedAt != nil || stage.FinishedAt != nil || stage.Logs != "" || stage.Error != "" || len(stage.Artifacts) != 0 || len(stage.Outputs) != 0 {
					t.Fatal("persistence rejection started or rewrote a stage")
				}
			}
			if adapters.builds.Load() != 0 || adapters.pushes.Load() != 0 || adapters.deploys.Load() != 0 || progress.Load() != 0 {
				t.Fatal("source persistence failure reached a stage adapter")
			}
			if errorStore != nil && errorStore.calls.Load() != 1 || fixture.checkouts != checkoutsBefore+1 {
				t.Fatal("boundary did not exercise one verified checkout/persistence call")
			}
			public, marshalErr := json.Marshal(stored)
			if marshalErr != nil || strings.Contains(string(public), canary) || strings.Contains(string(public), private) || strings.Contains(err.Error(), canary) {
				t.Fatal("raw recorder error or private path entered public/stored output")
			}
			if _, statErr := os.Stat(fixture.lastCheckout); !os.IsNotExist(statErr) {
				t.Fatal("private raw checkout was retained")
			}
			leftovers, globErr := filepath.Glob(filepath.Join(private, "cooker-native-source-*"))
			if globErr != nil || len(leftovers) != 0 {
				t.Fatal("private exported checkout was retained")
			}
		})
	}
}
