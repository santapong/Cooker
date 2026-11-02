package server

import (
	"context"
	"database/sql"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestRegistryExecutionBindsEveryQueueCallerAndRejectsMissingBacking(t *testing.T) {
	st := memory.New()
	authority := service.NewRegistryAuthorityService(st, nil)
	if err := wireRegistryExecution(&jobQueueDeps{}, authority, nil); err != nil {
		t.Fatal("disabled queue activated registry hooks")
	}
	if err := wireRegistryExecution(&jobQueueDeps{Enqueuer: service.NewJobQueueEnqueuer(jobqueue.NewMemory(), st.Pipelines)}, authority, nil); err == nil {
		t.Fatal("missing queue dependencies accepted")
	}
	// sql.Open does not connect. This tests exact pool/hook identity without
	// reading an owner database or attempting an external connection.
	db, err := sql.Open("postgres", "host=registry-fixture.invalid dbname=owned sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	jobs := jobqueue.NewPostgres(db)
	deps := &jobQueueDeps{DB: db, Store: jobs, Enqueuer: service.NewJobQueueEnqueuer(jobs, st.Pipelines), Runner: service.NewJobQueueRunner(st, service.NewExecutor(), nil), AppQueue: &service.AppDeployQueue{}}
	triggers := &service.TriggerService{Store: memory.NewTriggerStore()}
	if err := wireRegistryExecution(deps, authority, triggers); err == nil {
		t.Fatal("missing transactional admission hook accepted")
	}
	jobs.OnEnqueue = func(context.Context, *sql.Tx, *jobqueue.Job) error { return nil }
	deps.Runner.NativeAutomation = &service.PipelineAutomation{}
	if err := wireRegistryExecution(deps, authority, triggers); err != nil {
		t.Fatal(err)
	}
	if !deps.Enqueuer.RequireRegistryPins || deps.Enqueuer.PrepareRegistry == nil || deps.Enqueuer.AdmissionGuard == nil || deps.Runner.RegistryAuthority != authority || deps.AppQueue.RegistryAuthority != authority || !deps.AppQueue.RequireRegistryPins || deps.AppQueue.AdmissionGuard == nil {
		t.Fatal("queue caller omitted accepted authority")
	}
	if !deps.Runner.NativeAutomation.RequireRegistryPreflight || deps.Runner.NativeAutomation.RegistryPreflight == nil {
		t.Fatal("automation omitted before-source registry preflight")
	}
}
