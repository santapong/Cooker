package service

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/santapong/cooker/internal/model"
)

type StageSecretBinding struct {
	EnvironmentID string   `json:"environmentId"`
	Keys          []string `json:"keys"`
}

// PipelineSecretPlan is private admission metadata for authored native DAGs.
// Resolved values exist only in copied stage configs during execution.
type PipelineSecretPlan struct {
	Version      int                               `json:"version"`
	Stages       map[string]StageSecretBinding     `json:"stages"`
	Environments map[string]*EnvironmentSecretPlan `json:"environments"`
}

type acceptedPipelineSecretsKey struct{}
type acceptedPipelineSecrets struct{ Plan *PipelineSecretPlan }

func withAcceptedPipelineSecrets(ctx context.Context, plan *PipelineSecretPlan) context.Context {
	return context.WithValue(ctx, acceptedPipelineSecretsKey{}, acceptedPipelineSecrets{Plan: plan})
}

func pipelineSecretBindings(p *model.Pipeline) (map[string]StageSecretBinding, error) {
	bindings := map[string]StageSecretBinding{}
	if p == nil {
		return nil, fmt.Errorf("pipeline secret admission requires a definition")
	}
	for _, stage := range p.Stages {
		if len(stage.Config.SecretRefs) == 0 {
			continue
		}
		if stage.ID == "" || stage.EnvironmentID == "" || len(stage.Config.SecretRefs) > 256 {
			return nil, fmt.Errorf("stage secret references require an environment and bounded keys")
		}
		switch stage.Type {
		case model.StageTypeTest, model.StageTypeCustom:
		case model.StageTypeDeploy:
			if stage.Config.DeployRuntime != "docker" {
				return nil, fmt.Errorf("secret references are unavailable for this deployment runtime")
			}
		default:
			return nil, fmt.Errorf("runtime secrets cannot be injected into this stage type")
		}
		keys := slices.Clone(stage.Config.SecretRefs)
		sort.Strings(keys)
		for i, key := range keys {
			if !variablePattern.MatchString(key) || len(key) > 256 || i > 0 && key == keys[i-1] {
				return nil, fmt.Errorf("stage secret references are invalid")
			}
			if _, collision := stage.Config.Env[key]; collision {
				return nil, fmt.Errorf("stage config cannot override a referenced secret")
			}
		}
		if _, duplicate := bindings[stage.ID]; duplicate {
			return nil, fmt.Errorf("duplicate stage secret binding")
		}
		bindings[stage.ID] = StageSecretBinding{EnvironmentID: stage.EnvironmentID, Keys: keys}
	}
	return bindings, nil
}

func (e *Executor) PreparePipelineSecrets(ctx context.Context, pipeline *model.Pipeline) (*PipelineSecretPlan, error) {
	bindings, err := pipelineSecretBindings(pipeline)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, nil
	}
	if e == nil || e.envResolver == nil || e.envResolver.Environments == nil {
		return nil, fmt.Errorf("pipeline secret admission is unavailable")
	}
	plan := &PipelineSecretPlan{Version: 1, Stages: bindings, Environments: map[string]*EnvironmentSecretPlan{}}
	union := map[string]map[string]bool{}
	for _, binding := range bindings {
		if union[binding.EnvironmentID] == nil {
			union[binding.EnvironmentID] = map[string]bool{}
		}
		for _, key := range binding.Keys {
			union[binding.EnvironmentID][key] = true
		}
	}
	ids := make([]string, 0, len(union))
	for id := range union {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		env, err := e.envResolver.Environments.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("pipeline environment is unavailable")
		}
		keys := make([]string, 0, len(union[id]))
		for key := range union[id] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		plan.Environments[id], err = e.envResolver.PinEnvironmentSecrets(ctx, id, env.Version, keys, false)
		if err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func (e *Executor) resolvePipelineSecrets(ctx context.Context, pipeline *model.Pipeline) (map[string]map[string]string, error) {
	bindings, err := pipelineSecretBindings(pipeline)
	if err != nil {
		return nil, err
	}
	accepted, queued := ctx.Value(acceptedPipelineSecretsKey{}).(acceptedPipelineSecrets)
	plan := accepted.Plan
	if len(bindings) == 0 {
		if plan != nil {
			return nil, fmt.Errorf("accepted secret metadata does not match the pipeline")
		}
		return nil, nil
	}
	if !queued {
		plan, err = e.PreparePipelineSecrets(ctx, pipeline)
		if err != nil {
			return nil, err
		}
	}
	if plan == nil || plan.Version != 1 || len(plan.Stages) != len(bindings) || e == nil || e.envResolver == nil {
		return nil, fmt.Errorf("accepted pipeline has no valid immutable secret metadata")
	}
	for id, expected := range bindings {
		got, ok := plan.Stages[id]
		if !ok || got.EnvironmentID != expected.EnvironmentID || !slices.Equal(got.Keys, expected.Keys) {
			return nil, fmt.Errorf("accepted stage secret binding has changed")
		}
	}
	union := map[string][]string{}
	for _, binding := range bindings {
		for _, key := range binding.Keys {
			if !slices.Contains(union[binding.EnvironmentID], key) {
				union[binding.EnvironmentID] = append(union[binding.EnvironmentID], key)
			}
		}
	}
	for id, keys := range union {
		sort.Strings(keys)
		pin := plan.Environments[id]
		if pin == nil || !slices.Equal(pin.Keys, keys) {
			return nil, fmt.Errorf("accepted environment keys do not match stage permissions")
		}
	}
	resolved := map[string]*ResolvedAppEnv{}
	for _, binding := range bindings {
		if resolved[binding.EnvironmentID] != nil {
			continue
		}
		pin := plan.Environments[binding.EnvironmentID]
		if pin == nil {
			return nil, fmt.Errorf("accepted environment secret metadata is missing")
		}
		values, err := e.envResolver.ResolveDetailed(WithAcceptedSecretPlan(WithEnvironmentVersion(ctx, binding.EnvironmentID, pin.EnvironmentVersion), pin), &model.App{EnvironmentID: binding.EnvironmentID})
		if err != nil {
			return nil, err
		}
		RegisterLogSecrets(ctx, values.SecretValues...)
		resolved[binding.EnvironmentID] = values
	}
	if len(resolved) != len(plan.Environments) {
		return nil, fmt.Errorf("accepted secret metadata has unrelated environments")
	}
	stageValues := map[string]map[string]string{}
	for id, binding := range bindings {
		stageValues[id] = map[string]string{}
		for _, key := range binding.Keys {
			value, ok := resolved[binding.EnvironmentID].Values[key]
			if !ok {
				return nil, fmt.Errorf("accepted stage secret is unavailable")
			}
			stageValues[id][key] = value
		}
	}
	return stageValues, nil
}
