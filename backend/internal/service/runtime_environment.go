package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// ValidateRuntimeEnvironmentBinding also fences friendly-name aliases used by
// external secret managers. Missing rows may be saved in an unfinished draft;
// execution still requires the existing environment and accepted generation.
func ValidateRuntimeEnvironmentBinding(ctx context.Context, environments store.EnvironmentStore, id string) error {
	if id == "" {
		return nil
	}
	if model.IsRegistryCredentialEnvironmentID(id) {
		return fmt.Errorf("credential-only environments cannot be used by workloads")
	}
	if environments == nil {
		return fmt.Errorf("environment validation is unavailable")
	}
	env, err := environments.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("environment validation is unavailable")
	}
	if model.IsRegistryCredentialEnvironment(env) {
		return fmt.Errorf("credential-only environments cannot be used by workloads")
	}
	return nil
}

func ValidatePipelineRuntimeEnvironments(ctx context.Context, environments store.EnvironmentStore, p *model.Pipeline) error {
	if p == nil {
		return fmt.Errorf("pipeline definition is required")
	}
	seen := map[string]bool{}
	for _, stage := range p.Stages {
		if seen[stage.EnvironmentID] {
			continue
		}
		seen[stage.EnvironmentID] = true
		if err := ValidateRuntimeEnvironmentBinding(ctx, environments, stage.EnvironmentID); err != nil {
			return err
		}
	}
	return nil
}
