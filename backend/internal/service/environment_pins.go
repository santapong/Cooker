package service

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
)

// EnvironmentSecretPlan is private queue metadata. It contains names and
// provider version references, never secret values or their fingerprints.
// An explicit empty Keys list records admission-time absence; a missing plan
// cannot establish that absence for an older external-secret deployment.
type EnvironmentSecretPlan struct {
	Version            int                                 `json:"version"`
	EnvironmentID      string                              `json:"environmentId"`
	EnvironmentVersion int                                 `json:"environmentVersion"`
	Mode               string                              `json:"mode"`
	Keys               []string                            `json:"keys"`
	References         map[string]secrets.VersionReference `json:"references,omitempty"`
}

type acceptedSecretPlanKey struct{}
type acceptedSecretPlan struct{ Plan *EnvironmentSecretPlan }

func WithAcceptedSecretPlan(ctx context.Context, plan *EnvironmentSecretPlan) context.Context {
	return context.WithValue(ctx, acceptedSecretPlanKey{}, acceptedSecretPlan{Plan: plan})
}

func generationBoundManager(manager secrets.Manager) bool {
	bound, ok := manager.(interface{ EnvironmentGenerationBound() bool })
	return ok && bound.EnvironmentGenerationBound()
}

// PinEnvironmentSecrets observes the complete key set for Apps, or an explicit
// subset for authored stages. No worker discovers new keys or resolves latest
// versions after this metadata has been accepted into the durable queue.
func (r *AppEnvResolver) PinEnvironmentSecrets(ctx context.Context, id string, version int, requested []string, all bool) (*EnvironmentSecretPlan, error) {
	if id == "" {
		if len(requested) != 0 {
			return nil, fmt.Errorf("secret references require an environment")
		}
		return nil, nil
	}
	if r == nil || r.Environments == nil {
		return nil, fmt.Errorf("environment admission is unavailable")
	}
	if model.IsRegistryCredentialEnvironmentID(id) {
		return nil, fmt.Errorf("credential-only environments cannot be used by workloads")
	}
	env, err := r.Environments.Get(ctx, id)
	if err != nil || env.Version != version {
		return nil, fmt.Errorf("environment changed before secret admission")
	}
	if model.IsRegistryCredentialEnvironment(env) {
		return nil, fmt.Errorf("credential-only environments cannot be used by workloads")
	}
	plan := &EnvironmentSecretPlan{Version: 1, EnvironmentID: id, EnvironmentVersion: version, Mode: "none", Keys: []string{}}
	var available []string
	if r.Secrets != nil {
		available, err = r.Secrets.List(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("secret inventory is unavailable for admission")
		}
		plan.Mode = "external"
		if generationBoundManager(r.Secrets) {
			plan.Mode = "database"
		}
	} else if len(env.SecretKeys) != 0 || len(env.Secrets) != 0 {
		return nil, fmt.Errorf("secret backend is unavailable for admission")
	}
	if len(available) > 256 {
		return nil, fmt.Errorf("secret inventory exceeds the admission limit")
	}
	known := map[string]bool{}
	for _, key := range available {
		if !variablePattern.MatchString(key) || len(key) > 256 || known[key] {
			return nil, fmt.Errorf("secret inventory is invalid")
		}
		known[key] = true
	}
	if all {
		requested = available
	}
	if len(requested) > 256 {
		return nil, fmt.Errorf("too many secret references")
	}
	seen := map[string]bool{}
	for _, key := range requested {
		if !variablePattern.MatchString(key) || len(key) > 256 || seen[key] || !known[key] {
			return nil, fmt.Errorf("a secret reference is invalid or unavailable")
		}
		seen[key] = true
		plan.Keys = append(plan.Keys, key)
	}
	sort.Strings(plan.Keys)
	if plan.Mode == "external" {
		manager, ok := r.Secrets.(secrets.VersionedManager)
		if !ok {
			return nil, fmt.Errorf("secret backend cannot pin immutable versions")
		}
		plan.References = make(map[string]secrets.VersionReference, len(plan.Keys))
		for _, key := range plan.Keys {
			ref, err := manager.Pin(ctx, id, key)
			if err != nil {
				return nil, fmt.Errorf("an immutable secret version is unavailable for admission")
			}
			if ref.Validate() != nil || ref.Scope.EnvironmentID != id || ref.Scope.Key != key {
				return nil, fmt.Errorf("secret backend returned invalid version metadata")
			}
			plan.References[key] = ref
		}
	}
	current, err := r.Environments.Get(ctx, id)
	if err != nil || current.Version != version {
		return nil, fmt.Errorf("environment changed during secret admission")
	}
	return plan, nil
}

func (r *AppEnvResolver) acceptedSecretKeys(ctx context.Context, env *model.Environment) ([]string, *EnvironmentSecretPlan, bool, error) {
	accepted, ok := ctx.Value(acceptedSecretPlanKey{}).(acceptedSecretPlan)
	if !ok {
		return nil, nil, false, nil
	}
	plan := accepted.Plan
	if plan == nil {
		// Legacy database jobs still have the checked environment generation.
		// That guarantee does not exist for an external backend.
		if r.Secrets != nil && !generationBoundManager(r.Secrets) {
			return nil, nil, true, fmt.Errorf("accepted deployment has no immutable secret versions; review a new deployment")
		}
		return nil, nil, false, nil
	}
	if plan.Version != 1 || plan.EnvironmentID != env.ID || plan.EnvironmentVersion != env.Version || plan.Keys == nil || len(plan.Keys) > 256 {
		return nil, nil, true, fmt.Errorf("accepted secret metadata is invalid")
	}
	seen := map[string]bool{}
	for _, key := range plan.Keys {
		if !variablePattern.MatchString(key) || len(key) > 256 || seen[key] {
			return nil, nil, true, fmt.Errorf("accepted secret metadata is invalid")
		}
		seen[key] = true
	}
	switch plan.Mode {
	case "none":
		if len(plan.Keys) != 0 || len(plan.References) != 0 {
			return nil, nil, true, fmt.Errorf("accepted secret metadata is invalid")
		}
	case "database":
		if !generationBoundManager(r.Secrets) || len(plan.References) != 0 {
			return nil, nil, true, fmt.Errorf("accepted secret backend has changed")
		}
	case "external":
		if _, ok := r.Secrets.(secrets.VersionedManager); !ok || len(plan.References) != len(plan.Keys) {
			return nil, nil, true, fmt.Errorf("accepted secret backend is unavailable")
		}
		for _, key := range plan.Keys {
			ref, ok := plan.References[key]
			if !ok || ref.Validate() != nil || ref.Scope.EnvironmentID != env.ID || ref.Scope.Key != key {
				return nil, nil, true, fmt.Errorf("accepted secret metadata is invalid")
			}
		}
	default:
		return nil, nil, true, fmt.Errorf("accepted secret metadata is invalid")
	}
	return slices.Clone(plan.Keys), plan, true, nil
}
