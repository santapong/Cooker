package server

import (
	"context"
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// Bind registry metadata to the exact queue database pool. The ordinary store
// uses a separate pool over the same configured DATABASE_URL; only the queue
// pool can prove that validation and job admission share one transaction.
func wireRegistryExecution(deps *jobQueueDeps, authority *service.RegistryAuthorityService, triggers *service.TriggerService) error {
	if deps == nil || deps.Enqueuer == nil {
		return nil
	}
	if deps.DB == nil || deps.Runner == nil || authority == nil || triggers == nil || triggers.Store == nil {
		return fmt.Errorf("registry queue dependencies are unavailable")
	}
	guard, ok := triggers.Store.(store.NativeTriggerAdmissionGuard)
	if !ok {
		return fmt.Errorf("registry admission serialization is unavailable")
	}
	registries := storepg.NewRegistryConfigStore(deps.DB)
	environments := storepg.NewEnvironmentStore(deps.DB)
	if !storepg.RegistryAdmissionStoresMatch(deps.Store, registries, environments) {
		return fmt.Errorf("registry admission transaction backing is unavailable")
	}
	authority.Registries, authority.Environments = registries, environments
	deps.Enqueuer.AdmissionGuard = guard
	deps.Enqueuer.RequireRegistryPins = true
	deps.Enqueuer.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		return service.PreparePipelineRegistryAuthority(ctx, authority, p)
	}
	deps.Runner.RegistryAuthority = authority
	if native := deps.Runner.NativeAutomation; native != nil {
		native.RequireRegistryPreflight = true
		native.RegistryPreflight = func(ctx context.Context, p *model.Pipeline) error {
			return service.CheckPipelineRegistryPreflight(ctx, authority, p)
		}
	}
	if deps.AppQueue != nil {
		deps.AppQueue.RegistryAuthority = authority
		deps.AppQueue.RequireRegistryPins = true
		deps.AppQueue.AdmissionGuard = guard
	}
	return nil
}
