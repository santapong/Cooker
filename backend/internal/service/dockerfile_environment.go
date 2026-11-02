package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// DockerfileRuntimeService is the existing single-image deploy stage identity.
// An explicit absent/empty binding grants no linked runtime keys.
const DockerfileRuntimeService = "deploy"

func usesDockerfileEnvironment(plan *model.BuildPlan) bool {
	return usesServiceEnvironment(plan) && plan.Kind == model.BuildPlanDockerfile
}

func environmentPlainValues(env *model.Environment) map[string]string {
	if env == nil {
		return nil
	}
	if env.PlainVars != nil {
		return env.PlainVars
	}
	return env.Variables
}

// dockerfileSecretSelection checks names only, never retrieves secret values.
func dockerfileSecretSelection(app *model.App, env *model.Environment, protected []string) ([]string, error) {
	if app == nil || !usesDockerfileEnvironment(app.BuildPlan) || validateEnvironmentScopePlan(app.BuildPlan) != nil {
		return nil, fmt.Errorf("Dockerfile environment binding is invalid")
	}
	keys := app.BuildPlan.ServiceEnvironment[DockerfileRuntimeService]
	if len(app.BuildPlan.Variables) != 0 {
		return nil, fmt.Errorf("Dockerfile runtime scope does not grant Compose interpolation inputs")
	}
	if env == nil {
		if app.EnvironmentID != "" || len(keys) != 0 {
			return nil, fmt.Errorf("selected runtime keys require a linked environment")
		}
		return []string{}, nil
	}
	if env.ID != app.EnvironmentID || model.IsRegistryCredentialEnvironment(env) || len(protected) > 256 {
		return nil, fmt.Errorf("runtime environment metadata is unavailable")
	}
	plain := environmentPlainValues(env)
	secrets := make(map[string]bool, len(protected))
	for _, key := range protected {
		if !variablePattern.MatchString(key) || len(key) > 256 || secrets[key] {
			return nil, fmt.Errorf("protected key inventory is invalid")
		}
		secrets[key] = true
	}
	for key := range app.BuildPlan.Args {
		_, readable := plain[key]
		if readable || secrets[key] {
			return nil, fmt.Errorf("linked runtime keys cannot be build arguments")
		}
	}
	selected := []string{}
	for _, key := range keys {
		_, readable := plain[key]
		if readable && secrets[key] {
			return nil, fmt.Errorf("a runtime key is both readable and protected")
		}
		if !readable && !secrets[key] {
			return nil, fmt.Errorf("selected runtime key is unavailable")
		}
		if secrets[key] {
			selected = append(selected, key)
		}
	}
	sort.Strings(selected)
	return selected, nil
}

// pinDockerfileEnvironment records the selected secret subset. Explicit empty
// bindings do not list providers or discover credentials; absence is evidence.
func (r *AppEnvResolver) pinDockerfileEnvironment(ctx context.Context, app *model.App, env *model.Environment) (*EnvironmentSecretPlan, error) {
	if env == nil {
		_, err := dockerfileSecretSelection(app, nil, nil)
		return nil, err
	}
	if r == nil || r.Environments == nil {
		return nil, fmt.Errorf("runtime environment admission is unavailable")
	}
	var protected []string
	if len(app.BuildPlan.ServiceEnvironment[DockerfileRuntimeService]) != 0 && r.Secrets != nil {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var err error
		protected, err = r.Secrets.List(requestCtx, env.ID)
		if err != nil {
			return nil, fmt.Errorf("protected key inventory is unavailable for admission")
		}
	} else if len(app.BuildPlan.ServiceEnvironment[DockerfileRuntimeService]) != 0 {
		for key := range env.Secrets {
			protected = append(protected, key)
		}
		protected = append(protected, env.SecretKeys...)
		// Redacted and raw stores may report the same names. Normalize metadata.
		sort.Strings(protected)
		protected = slices.Compact(protected)
	}
	selected, err := dockerfileSecretSelection(app, env, protected)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		current, err := r.Environments.Get(ctx, env.ID)
		if err != nil || current.Version != env.Version {
			return nil, fmt.Errorf("environment changed during runtime key admission")
		}
		return &EnvironmentSecretPlan{Version: 1, EnvironmentID: env.ID, EnvironmentVersion: env.Version, Mode: "none", Keys: []string{}}, nil
	}
	return r.PinEnvironmentSecrets(ctx, env.ID, env.Version, selected, false)
}

// validateDockerfileSecretEvidence is metadata-only. Workers never repin or
// discover newly added provider keys while resolving an accepted deployment.
func (r *AppEnvResolver) validateDockerfileSecretEvidence(ctx context.Context, app *model.App, env *model.Environment) error {
	keys, _, pinned, err := r.acceptedSecretKeys(ctx, env)
	if err != nil || !pinned {
		return fmt.Errorf("accepted Dockerfile runtime key evidence is unavailable")
	}
	selected, err := dockerfileSecretSelection(app, env, keys)
	if err != nil || !slices.Equal(selected, keys) {
		return fmt.Errorf("accepted Dockerfile runtime keys do not match the reviewed selection")
	}
	return nil
}

func validateDockerfileBuildValues(ctx context.Context, app *model.App) error {
	if app == nil || !usesDockerfileEnvironment(app.BuildPlan) {
		return nil
	}
	if rejectScopedSecretValue(ctx, app.BuildPlan.Args, nil) != nil {
		return fmt.Errorf("protected runtime values cannot be build arguments")
	}
	return nil
}
