package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/santapong/cooker/internal/governance"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
)

// Native source execution exists only behind durable queue admission. Inline
// execution remains unable to turn a source binding into a host-directory build.
func wireNativeSourceAutomation(st *store.Store, connections *connection.Service, source service.PipelineSourceProvider, deps *jobQueueDeps, triggers *service.TriggerService, gov *governance.Client) (*service.PipelineAutomation, error) {
	if deps == nil || deps.Enqueuer == nil {
		return nil, nil
	}
	if st == nil || connections == nil || source == nil || deps.Runner == nil || deps.Store == nil || triggers == nil || triggers.Store == nil {
		return nil, fmt.Errorf("native source queue dependencies are unavailable")
	}
	guard, ok := triggers.Store.(store.NativeTriggerAdmissionGuard)
	if !ok {
		return nil, fmt.Errorf("native source admission serialization is unavailable")
	}
	preparer := &service.PipelineSourcePreparer{Store: st, Connections: connections, Source: source}
	native := &service.PipelineAutomation{Preparer: preparer, Triggers: triggers, Jobs: deps.Store, Enqueue: deps.Enqueuer.EnqueueSourceRun, Authorize: nativeAutomationAuthorizer(st, gov)}
	deps.Enqueuer.AdmissionGuard = guard
	deps.Enqueuer.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
		if p == nil || p.Source == nil {
			return nil, nil
		}
		return preparer.Prepare(ctx, p, p.Source.FullRef, "manual")
	}
	deps.Runner.SourcePreparer = preparer
	deps.Runner.NativeAutomation = native
	return native, nil
}

func nativeAutomationAuthorizer(st *store.Store, gov *governance.Client) func(context.Context, *model.TriggerRule, *model.Pipeline) error {
	return func(ctx context.Context, r *model.TriggerRule, p *model.Pipeline) error {
		if st == nil || st.Environments == nil || r == nil || r.ID == "" || p == nil || !r.Enabled || r.ActorSubject != "cooker:trigger:"+r.ID || len(r.ActorGroups) != 0 {
			return fmt.Errorf("invalid native automation actor")
		}
		kind, id := r.Target()
		if kind != "pipeline" || id != p.ID || service.ValidatePipelineSource(p) != nil || p.Source == nil {
			return fmt.Errorf("native automation target is unavailable")
		}
		if service.ValidatePipelineRuntimeEnvironments(ctx, st.Environments, p) != nil {
			return fmt.Errorf("native automation environment is unavailable")
		}
		if gov == nil || !gov.Enabled() {
			return nil
		}
		if !gov.DelegationEnabled() {
			return fmt.Errorf("governance delegation is required for automation")
		}
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("native pipeline service name is unavailable")
		}
		envs := map[string]bool{}
		for _, stage := range p.Stages {
			if stage.Type == model.StageTypeDeploy && stage.EnvironmentID == "" {
				return fmt.Errorf("governed automation requires an explicit deployment environment")
			}
			if stage.EnvironmentID != "" {
				envs[stage.EnvironmentID] = true
			}
		}
		ids := make([]string, 0, len(envs))
		for id := range envs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			env, err := st.Environments.Get(ctx, id)
			if err != nil || env == nil || strings.TrimSpace(env.Name) == "" {
				return fmt.Errorf("native automation environment is unavailable")
			}
			decision, err := gov.AuthorizeOnBehalf(ctx, governance.PreresolvedActor{Kind: "service", ID: r.ActorSubject}, strings.ToLower(p.Name), strings.ToLower(env.Name), r.ID)
			if err != nil || !decision.Allowed() {
				return fmt.Errorf("native automation authorization denied")
			}
		}
		return nil
	}
}
