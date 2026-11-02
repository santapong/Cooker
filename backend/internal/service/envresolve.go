package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
)

// AppEnvResolver resolves the runtime environment an App's linked
// Environment contributes to its deployed workloads: the Environment's
// PlainVars plus its Secrets (fetched decrypted from the configured
// secrets.Manager). Secrets override PlainVars on key collision; the
// caller overlays any stage-explicit env on top (stage wins) via
// mergeEnv.
//
// Unlinked apps resolve to an empty map. A linked environment requires its
// store; configured secrets require their backend rather than disappearing.
type AppEnvResolver struct {
	Environments store.EnvironmentStore
	Secrets      secrets.Manager
}

type environmentVersionKey struct{}
type expectedEnvironmentVersion struct {
	ID      string
	Version int
}

// WithEnvironmentVersion pins the linked configuration generation selected
// when an attempt was accepted. This is not an external secret version pin.
func WithEnvironmentVersion(ctx context.Context, id string, version int) context.Context {
	return context.WithValue(ctx, environmentVersionKey{}, expectedEnvironmentVersion{ID: id, Version: version})
}

// Resolve returns the merged PlainVars+Secrets map for app's linked
// Environment. Secret VALUES are returned decrypted — callers must
// never log them (log key counts, not contents).
func (r *AppEnvResolver) Resolve(ctx context.Context, app *model.App) (map[string]string, error) {
	resolved, err := r.ResolveDetailed(ctx, app)
	if err != nil {
		return nil, err
	}
	RegisterLogSecrets(ctx, resolved.SecretValues...)
	return resolved.Values, nil
}

// ResolvedAppEnv is ephemeral execution data, never a serialized plan or API
// response. SecretValues is deliberately separate from ordinary configuration
// so log protection does not need to guess sensitivity from key names.
type ResolvedAppEnv struct {
	Values             map[string]string `json:"-"`
	SecretValues       []string          `json:"-"`
	EnvironmentVersion int               `json:"environmentVersion"`
}

func (r *AppEnvResolver) ResolveDetailed(ctx context.Context, app *model.App) (*ResolvedAppEnv, error) {
	resolved := &ResolvedAppEnv{Values: map[string]string{}}
	if app == nil || app.EnvironmentID == "" {
		if app != nil && usesDockerfileEnvironment(app.BuildPlan) {
			if _, err := dockerfileSecretSelection(app, nil, nil); err != nil {
				return nil, err
			}
		}
		return resolved, nil
	}
	if model.IsRegistryCredentialEnvironmentID(app.EnvironmentID) {
		return nil, fmt.Errorf("credential-only environments cannot be used by workloads")
	}
	if r == nil || r.Environments == nil {
		return nil, fmt.Errorf("resolve app env: environment store is not configured")
	}
	env, err := r.Environments.Get(ctx, app.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("resolve app env: environment %s: %w", app.EnvironmentID, err)
	}
	if model.IsRegistryCredentialEnvironment(env) {
		return nil, fmt.Errorf("credential-only environments cannot be used by workloads")
	}
	env.NormalisePlainVars()
	if expected, ok := ctx.Value(environmentVersionKey{}).(expectedEnvironmentVersion); ok && (expected.ID != env.ID || expected.Version != env.Version) {
		return nil, fmt.Errorf("environment changed after queueing; review and start a new deployment")
	}
	resolved.EnvironmentVersion = env.Version
	if usesDockerfileEnvironment(app.BuildPlan) {
		if _, accepted := ctx.Value(acceptedSecretPlanKey{}).(acceptedSecretPlan); !accepted {
			pin, err := r.pinDockerfileEnvironment(ctx, app, env)
			if err != nil {
				return nil, err
			}
			ctx = WithAcceptedSecretPlan(ctx, pin)
		}
		if err := r.validateDockerfileSecretEvidence(ctx, app, env); err != nil {
			return nil, err
		}
	}

	out := make(map[string]string, len(env.PlainVars))
	for k, v := range env.PlainVars {
		if usesDockerfileEnvironment(app.BuildPlan) && !allowsEnvironmentKey(app.BuildPlan, DockerfileRuntimeService, k) {
			continue
		}
		out[k] = v
	}
	resolved.Values = out
	keys, pin, pinned, err := r.acceptedSecretKeys(ctx, env)
	if err != nil {
		return nil, err
	}
	if r.Secrets == nil && !pinned {
		if len(env.SecretKeys) > 0 || len(env.Secrets) > 0 {
			return nil, fmt.Errorf("resolve app env: secret backend is not configured")
		}
	}
	if !pinned && r.Secrets != nil {
		keys, err = r.Secrets.List(ctx, env.ID)
		if err != nil {
			return nil, redactedLogError{cause: err, text: fmt.Sprintf("resolve app env: secret backend unavailable for %s", env.ID)}
		}
	}
	sort.Strings(keys) // deterministic fetch order for stable logs/tests
	for _, k := range keys {
		if usesServiceEnvironment(app.BuildPlan) {
			if _, collision := env.PlainVars[k]; collision {
				return nil, fmt.Errorf("environment key %s is configured as both plain and secret", k)
			}
		}
		var v []byte
		if pin != nil && pin.Mode == "external" {
			v, err = secrets.ReadPinned(ctx, r.Secrets, env.ID, k, pin.References[k])
		} else {
			v, err = r.Secrets.Get(ctx, env.ID, k)
		}
		if err != nil {
			return nil, redactedLogError{cause: err, text: fmt.Sprintf("resolve app env: secret %s/%s unavailable", env.ID, k)}
		}
		out[k] = string(v)
		if len(v) > 0 {
			resolved.SecretValues = append(resolved.SecretValues, string(v))
		}
	}
	// The database secret manager resolves each value separately. Recheck after
	// all reads so a rotation/config edit cannot produce a mixed generation.
	current, err := r.Environments.Get(ctx, env.ID)
	if err != nil || current.Version != resolved.EnvironmentVersion {
		return nil, fmt.Errorf("environment changed while resolving configuration; review and start a new deployment")
	}
	return resolved, nil
}

// mergeEnv overlays override onto base without mutating either.
// Override keys win. Nil-safe on both sides; returns nil only when
// both inputs are empty so synthesized StageConfigs stay omitempty-
// friendly.
func mergeEnv(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}
