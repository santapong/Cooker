package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/compose-spec/compose-go/v2/template"
	"github.com/santapong/cooker/internal/model"
)

const ServiceEnvironmentScope = "service"

func usesServiceEnvironment(plan *model.BuildPlan) bool {
	return plan != nil && plan.EnvironmentScope == ServiceEnvironmentScope
}

func validateEnvironmentScopePlan(plan *model.BuildPlan) error {
	if plan == nil {
		return nil
	}
	if plan.EnvironmentScope != "" && plan.EnvironmentScope != ServiceEnvironmentScope {
		return fmt.Errorf("unknown environment scope")
	}
	if !usesServiceEnvironment(plan) {
		if len(plan.ServiceEnvironment) > 0 {
			return fmt.Errorf("service environment requires explicit service scope")
		}
		return nil
	}
	if plan.Kind != model.BuildPlanCompose && plan.Kind != model.BuildPlanDockerfile {
		return fmt.Errorf("service environment scope requires a Compose or Dockerfile plan")
	}
	if len(plan.ServiceEnvironment) > 128 {
		return fmt.Errorf("too many service environment scopes")
	}
	for name, keys := range plan.ServiceEnvironment {
		if plan.Kind == model.BuildPlanDockerfile && name != DockerfileRuntimeService {
			return fmt.Errorf("Dockerfile environment scope requires the deploy component")
		}
		if name == "" || len(name) > 128 || len(keys) > 256 {
			return fmt.Errorf("invalid service environment scope")
		}
		seen := map[string]bool{}
		for _, key := range keys {
			if !variablePattern.MatchString(key) || len(key) > 256 || seen[key] {
				return fmt.Errorf("invalid or duplicate environment key for service %s", name)
			}
			seen[key] = true
		}
	}
	return nil
}

func serviceEnvironment(plan *model.BuildPlan, name string, all map[string]string) (map[string]string, error) {
	if !usesServiceEnvironment(plan) {
		return all, nil
	}
	selected := map[string]string{}
	for _, key := range plan.ServiceEnvironment[name] {
		value, ok := all[key]
		if !ok {
			return nil, fmt.Errorf("service %s references unavailable environment key %s", name, key)
		}
		selected[key] = value
	}
	return selected, nil
}

func allowsEnvironmentKey(plan *model.BuildPlan, name, key string) bool {
	for _, candidate := range plan.ServiceEnvironment[name] {
		if candidate == key {
			return true
		}
	}
	return false
}

// Linked environment values are runtime inputs. In explicit mode they can be
// interpolated only in the selected service's environment, never in a build,
// image, path, command, label or global resource. Public interpolation inputs
// remain available separately through BuildPlan.Variables.
func validateRawScopedCompose(ctx context.Context, app *model.App, env map[string]string, raw map[string]any) error {
	plan := app.BuildPlan
	if err := validateEnvironmentScopePlan(plan); err != nil {
		return err
	}
	if !usesServiceEnvironment(plan) {
		return nil
	}
	for key := range plan.Variables {
		if _, collision := env[key]; collision {
			return fmt.Errorf("compose input %s collides with a linked environment key", key)
		}
	}
	checkRefs := func(value any, service string, runtime bool) error {
		for key := range template.ExtractVariables(map[string]any{"value": value}, nil) {
			if _, linked := env[key]; !linked {
				continue
			}
			if !runtime || !allowsEnvironmentKey(plan, service, key) {
				return fmt.Errorf("linked environment key %s is outside service %s runtime scope", key, service)
			}
		}
		return nil
	}
	for field, value := range raw {
		if field != "services" {
			if err := checkRefs(value, "", false); err != nil {
				return err
			}
			if err := rejectScopedSecretValue(ctx, value, nil); err != nil {
				return err
			}
			continue
		}
		services, _ := value.(map[string]any)
		for name, value := range services {
			svc, _ := value.(map[string]any)
			for key, field := range svc {
				if key == "env_file" {
					return fmt.Errorf("service %s: env_file is not supported with explicit environment scope; declare reviewed runtime keys", name)
				}
				if err := checkRefs(field, name, key == "environment"); err != nil {
					return err
				}
				var selected map[string]string
				if key == "environment" {
					var err error
					selected, err = serviceEnvironment(plan, name, env)
					if err != nil {
						return err
					}
				}
				if err := rejectScopedSecretValue(ctx, field, selected); err != nil {
					return fmt.Errorf("service %s: %w", name, err)
				}
				// Bare entries inherit from the loader's environment without a
				// ${...} expression, so validate those separately.
				if key == "environment" {
					for inherited := range inheritedComposeKeys(field) {
						if _, linked := env[inherited]; linked && !allowsEnvironmentKey(plan, name, inherited) {
							return fmt.Errorf("linked environment key %s is outside service %s runtime scope", inherited, name)
						}
					}
				}
				if key == "build" {
					if build, ok := field.(map[string]any); ok {
						for inherited := range composeMappingKeys(build["args"]) {
							if _, linked := env[inherited]; linked {
								return fmt.Errorf("linked environment key %s cannot be a build argument", inherited)
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func composeMappingKeys(value any) map[string]bool {
	keys := map[string]bool{}
	switch values := value.(type) {
	case map[string]any:
		for key := range values {
			keys[key] = true
		}
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok {
				key, _, _ := strings.Cut(text, "=")
				keys[key] = true
			}
		}
	}
	return keys
}

func inheritedComposeKeys(value any) map[string]bool {
	keys := map[string]bool{}
	switch values := value.(type) {
	case map[string]any:
		for key, value := range values {
			if value == nil {
				keys[key] = true
			}
		}
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok && !strings.Contains(text, "=") {
				keys[text] = true
			}
		}
	}
	return keys
}

func containsKnownValue(value any, secret string) bool {
	if secret == "" {
		return false
	}
	switch v := value.(type) {
	case string:
		return strings.Contains(v, secret)
	case map[string]any:
		for key, item := range v {
			if strings.Contains(key, secret) || containsKnownValue(item, secret) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if containsKnownValue(item, secret) {
				return true
			}
		}
	}
	return false
}

// Check values as well as references: aliases and literal copies must not
// bypass an allowlist. This covers exact known values, not arbitrary encodings.
func rejectScopedSecretValue(ctx context.Context, value any, selected map[string]string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return err
	}
	for _, secret := range logSecretValues(ctx) {
		allowed := false
		for _, value := range selected {
			if value == secret {
				allowed = true
				break
			}
		}
		if !allowed && containsKnownValue(generic, secret) {
			return fmt.Errorf("known secret value appears outside its allowed runtime environment")
		}
	}
	return nil
}

func validateResolvedEnvironmentScope(ctx context.Context, app *model.App, result *resolvedCompose, env map[string]string) error {
	plan := app.BuildPlan
	if !usesServiceEnvironment(plan) {
		return nil
	}
	for name := range plan.ServiceEnvironment {
		if _, ok := result.Project.Services[name]; !ok {
			return fmt.Errorf("environment scope references unselected service %s", name)
		}
		if _, err := serviceEnvironment(plan, name, env); err != nil {
			return err
		}
	}
	if err := rejectScopedSecretValue(ctx, plan.Variables, nil); err != nil {
		return fmt.Errorf("compose inputs: %w", err)
	}
	if err := rejectScopedSecretValue(ctx, plan.Args, nil); err != nil {
		return fmt.Errorf("build arguments: %w", err)
	}
	for key := range plan.Args {
		if _, linked := env[key]; linked {
			return fmt.Errorf("linked environment key %s cannot be a build argument", key)
		}
	}
	for name, svc := range result.Project.Services {
		selected, err := serviceEnvironment(plan, name, env)
		if err != nil {
			return err
		}
		if err := rejectScopedSecretValue(ctx, svc.Environment, selected); err != nil {
			return fmt.Errorf("service %s: %w", name, err)
		}
		svc.Environment = nil
		if err := rejectScopedSecretValue(ctx, svc, nil); err != nil {
			return fmt.Errorf("service %s: %w", name, err)
		}
	}
	for _, svc := range result.Graph.Services {
		selected, err := serviceEnvironment(plan, svc.Name, env)
		if err != nil {
			return err
		}
		if err := rejectScopedSecretValue(ctx, svc.Environment, selected); err != nil {
			return fmt.Errorf("service %s: %w", svc.Name, err)
		}
	}
	return nil
}
