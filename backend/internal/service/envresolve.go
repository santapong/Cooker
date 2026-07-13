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
// Nil-safety: a nil resolver, an app with no EnvironmentID, or a
// missing store/manager all resolve to an empty map — deploys of
// unlinked apps are unchanged.
type AppEnvResolver struct {
	Environments store.EnvironmentStore
	Secrets      secrets.Manager
}

// Resolve returns the merged PlainVars+Secrets map for app's linked
// Environment. Secret VALUES are returned decrypted — callers must
// never log them (log key counts, not contents).
func (r *AppEnvResolver) Resolve(ctx context.Context, app *model.App) (map[string]string, error) {
	if r == nil || app == nil || app.EnvironmentID == "" || r.Environments == nil {
		return map[string]string{}, nil
	}
	env, err := r.Environments.Get(ctx, app.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("resolve app env: environment %s: %w", app.EnvironmentID, err)
	}
	env.NormalisePlainVars()

	out := make(map[string]string, len(env.PlainVars))
	for k, v := range env.PlainVars {
		out[k] = v
	}
	if r.Secrets == nil {
		return out, nil
	}
	keys, err := r.Secrets.List(ctx, env.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve app env: list secrets for %s: %w", env.ID, err)
	}
	sort.Strings(keys) // deterministic fetch order for stable logs/tests
	for _, k := range keys {
		v, err := r.Secrets.Get(ctx, env.ID, k)
		if err != nil {
			return nil, fmt.Errorf("resolve app env: secret %s/%s: %w", env.ID, k, err)
		}
		out[k] = string(v)
	}
	return out, nil
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
