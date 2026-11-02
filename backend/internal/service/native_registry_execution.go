package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/santapong/cooker/internal/build/registryauth"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/model"
)

type acceptedRegistryAuthorityKey struct{}
type acceptedRegistryAuthority struct {
	Subject model.RegistrySubject
	Plan    *model.RegistryAuthorityPlan
	Invalid bool
}

// WithAcceptedRegistryAuthority records explicit immutable authority. Nil is
// missing evidence, never anonymous. The trusted App/native caller supplies the
// subject; copying prevents later pointer edits from expanding stage access.
func WithAcceptedRegistryAuthority(ctx context.Context, subject model.RegistrySubject, plan *model.RegistryAuthorityPlan) context.Context {
	accepted := acceptedRegistryAuthority{Subject: subject}
	if plan != nil {
		if plan.Validate() != nil {
			accepted.Invalid = true
		} else {
			data, err := json.Marshal(plan)
			var snapshot model.RegistryAuthorityPlan
			if err != nil || json.Unmarshal(data, &snapshot) != nil {
				accepted.Invalid = true
			} else {
				accepted.Plan = &snapshot
			}
		}
	}
	return context.WithValue(ctx, acceptedRegistryAuthorityKey{}, accepted)
}

func WithRegistryAuthority(authority *RegistryAuthorityService) Option {
	return func(e *Executor) { e.registryAuthority = authority }
}

// validateNativeRegistryTransports is a pure capability preflight for the
// immutable native pipeline. Even an anonymous accepted plan must use a scoped
// transport. Keep this separate from receipt validation: older accepted jobs
// remain readable without repinning, but cannot execute unsupported stages.
func validateNativeRegistryTransports(p *model.Pipeline) error {
	if p == nil {
		return model.ErrInvalidRegistryAuthority
	}
	for _, stage := range p.Stages {
		if stage.Type == model.StageTypeDeploy && stage.Config.DeployRuntime != "docker" && stage.Config.DeployRuntime != "compose" {
			return fmt.Errorf("%w: accepted registry transport is unavailable for this deployment runtime", deployer.ErrUnavailable)
		}
	}
	return nil
}

// PipelineRegistrySelection derives the exact union and every stage binding.
// Empty selections authorize anonymous registry access only.
func PipelineRegistrySelection(p *model.Pipeline) ([]string, map[string][]string, error) {
	if p == nil || (model.RegistrySubject{Kind: "pipeline", ID: p.ID}).Validate() != nil || strings.HasPrefix(p.ID, "vflow_") || strings.HasPrefix(p.ID, "vapp_") || len(p.Stages) > 256 {
		return nil, nil, model.ErrInvalidRegistryAuthority
	}
	bindings := map[string][]string{}
	union := map[string]bool{}
	for _, stage := range p.Stages {
		if !model.ValidRegistryIDs([]string{stage.ID}, 1) || !model.ValidRegistryIDs(stage.Config.RegistryConnectionIDs, 32) {
			return nil, nil, model.ErrInvalidRegistryAuthority
		}
		if _, exists := bindings[stage.ID]; exists {
			return nil, nil, model.ErrInvalidRegistryAuthority
		}
		ids := slices.Clone(stage.Config.RegistryConnectionIDs)
		sort.Strings(ids)
		bindings[stage.ID] = ids
		for _, id := range ids {
			union[id] = true
		}
	}
	ids := make([]string, 0, len(union))
	for id := range union {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if !model.ValidRegistryIDs(ids, 32) {
		return nil, nil, model.ErrInvalidRegistryAuthority
	}
	return ids, bindings, nil
}

func PreparePipelineRegistryAuthority(ctx context.Context, authority *RegistryAuthorityService, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
	if err := validateNativeRegistryTransports(p); err != nil {
		return nil, err
	}
	ids, bindings, err := PipelineRegistrySelection(p)
	if err != nil {
		return nil, err
	}
	if authority != nil {
		if err := ValidatePipelineRuntimeEnvironments(ctx, authority.Environments, p); err != nil {
			return nil, err
		}
	}
	return authority.Prepare(ctx, model.RegistrySubject{Kind: "pipeline", ID: p.ID}, ids, bindings)
}

func (e *Executor) PreparePipelineRegistry(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
	if e == nil {
		return nil, ErrRegistryAuthorityRequired
	}
	return PreparePipelineRegistryAuthority(ctx, e.registryAuthority, p)
}

func validateNativeRegistryPlan(p *model.Pipeline, plan *model.RegistryAuthorityPlan) error {
	_, bindings, err := PipelineRegistrySelection(p)
	if err != nil || plan == nil || plan.Validate() != nil || plan.Subject != (model.RegistrySubject{Kind: "pipeline", ID: p.ID}) || len(plan.Stages) != len(bindings) {
		return model.ErrInvalidRegistryAuthority
	}
	for stage, expected := range bindings {
		actual, exists := plan.Stages[stage]
		actual = slices.Clone(actual)
		sort.Strings(actual)
		if !exists || !slices.Equal(actual, expected) {
			return model.ErrInvalidRegistryAuthority
		}
	}
	return nil
}

func (e *JobQueueEnqueuer) prepareRegistryPlan(ctx context.Context, plan *runJobPayload) error {
	if plan.Pipeline == nil {
		if e.RequireRegistryPins {
			return ErrRegistryAuthorityRequired
		}
		return nil
	}
	if e.PrepareRegistry == nil {
		if e.RequireRegistryPins {
			return ErrRegistryAuthorityRequired
		}
		for _, stage := range plan.Pipeline.Stages {
			if len(stage.Config.RegistryConnectionIDs) != 0 {
				return ErrRegistryAuthorityRequired
			}
		}
		return nil // unconfigured legacy fixtures carry no credential authority
	}
	if err := validateNativeRegistryTransports(plan.Pipeline); err != nil {
		return err
	}
	var err error
	plan.Registry, err = e.PrepareRegistry(ctx, plan.Pipeline)
	if err != nil {
		return fmt.Errorf("native registry authority could not be prepared")
	}
	if validateNativeRegistryPlan(plan.Pipeline, plan.Registry) != nil {
		return model.ErrInvalidRegistryAuthority
	}
	return nil
}

func (e *Executor) prepareExecutionRegistry(ctx context.Context, p *model.Pipeline) (context.Context, error) {
	accepted, ok := ctx.Value(acceptedRegistryAuthorityKey{}).(acceptedRegistryAuthority)
	if !ok {
		if e.registryAuthority == nil {
			if p == nil {
				return ctx, model.ErrInvalidRegistryAuthority
			}
			for _, stage := range p.Stages {
				if len(stage.Config.RegistryConnectionIDs) != 0 {
					return ctx, ErrRegistryAuthorityRequired
				}
			}
			return ctx, nil // source-compatible, unconfigured fixture execution
		}
		plan, err := e.PreparePipelineRegistry(ctx, p)
		if err != nil {
			return ctx, err
		}
		ctx = WithAcceptedRegistryAuthority(ctx, model.RegistrySubject{Kind: "pipeline", ID: p.ID}, plan)
		accepted, _ = ctx.Value(acceptedRegistryAuthorityKey{}).(acceptedRegistryAuthority)
	}
	if accepted.Invalid || accepted.Plan == nil {
		return ctx, ErrRegistryAuthorityRequired
	}
	if accepted.Subject.Kind == "pipeline" {
		if err := validateNativeRegistryTransports(p); err != nil {
			return ctx, err
		}
		if validateNativeRegistryPlan(p, accepted.Plan) != nil {
			return ctx, model.ErrInvalidRegistryAuthority
		}
	} else if accepted.Subject.Kind != "app" || accepted.Subject.Validate() != nil || accepted.Plan.Subject != accepted.Subject || accepted.Plan.Validate() != nil {
		return ctx, model.ErrInvalidRegistryAuthority
	}
	if err := e.registryAuthority.ValidateCurrent(ctx, accepted.Subject, accepted.Plan); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func (e *Executor) stageRegistry(ctx context.Context, stage *model.Stage) (registryauth.Resolver, []string, error) {
	accepted, ok := ctx.Value(acceptedRegistryAuthorityKey{}).(acceptedRegistryAuthority)
	if !ok {
		return e.registryAuth, nil, nil
	} // never used by configured accepted workers
	if accepted.Invalid || accepted.Plan == nil {
		return nil, nil, ErrRegistryAuthorityRequired
	}
	resolve, err := e.registryAuthority.Resolver(ctx, accepted.Subject, accepted.Plan, stage.ID)
	if err != nil {
		return nil, nil, err
	}
	hosts, err := RegistryAuthorityHosts(accepted.Plan, stage.ID)
	return resolve, hosts, err
}

// AcceptedRunSnapshot materializes only the durable admission view. It neither
// repins registry/source versions nor claims a checkout occurred.
func (e *JobQueueEnqueuer) AcceptedRunSnapshot(ctx context.Context, runID string) (*model.PipelineRun, error) {
	if e == nil || e.store == nil || runID == "" {
		return nil, fmt.Errorf("accepted run snapshot is unavailable")
	}
	job, err := e.store.Get(ctx, "run:"+runID)
	if err != nil {
		return nil, fmt.Errorf("accepted run snapshot is unavailable")
	}
	var payload runJobPayload
	if json.Unmarshal(job.Payload, &payload) != nil || job.ID != "run:"+runID || job.Kind != JobKindPipelineRun || payload.RunID != runID || payload.Pipeline == nil || payload.Pipeline.ID != payload.PipelineID || payload.Run == nil || payload.Run.ID != runID || payload.Run.PipelineID != payload.PipelineID || payload.Run.PipelineVersion != payload.Pipeline.Version || payload.Run.Status != model.RunStatusPending || payload.Run.CreatedAt.IsZero() || payload.Run.StartedAt != nil || payload.Run.FinishedAt != nil {
		return nil, fmt.Errorf("accepted run snapshot is invalid")
	}
	run := payload.Run.Clone()
	if payload.Pipeline.Source != nil {
		if payload.Source == nil || ValidatePipelineSourcePlan(payload.Pipeline, payload.Source) != nil || run.Source != nil && run.Source.CheckedOutAt != nil {
			return nil, fmt.Errorf("accepted source snapshot is invalid")
		}
		receipt, err := model.RunSourceFromAcceptedPlan(payload.Source)
		if err != nil {
			return nil, fmt.Errorf("accepted source snapshot is invalid")
		}
		run.Source, err = model.MergeRunSource(run.Source, receipt)
		if err != nil {
			return nil, fmt.Errorf("accepted source snapshot is invalid")
		}
	} else if payload.Source != nil || payload.Trigger != nil || run.Source != nil {
		return nil, fmt.Errorf("accepted source snapshot is invalid")
	}
	if payload.Registry != nil && validateNativeRegistryPlan(payload.Pipeline, payload.Registry) != nil {
		return nil, model.ErrInvalidRegistryAuthority
	}
	if e.RequireRegistryPins && payload.Registry == nil {
		return nil, ErrRegistryAuthorityRequired
	}
	return run, nil
}
