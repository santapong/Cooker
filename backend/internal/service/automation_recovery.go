package service

import (
	"context"
	"fmt"
	"reflect"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// reviewedRecoveryApp checks metadata and machine authority without reading a
// source credential, resolving values or recording a delivery. Dispatch and
// worker admission retain their own checks after the inbox boundary.
func (a *Automation) reviewedRecoveryApp(ctx context.Context, r *model.TriggerRule, fullRef string) (*model.App, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("source recovery was cancelled")
	}
	if a == nil || a.Store == nil || a.Store.Apps == nil || a.Connections == nil || a.Connections.Store == nil || a.Source == nil || a.Triggers == nil || a.Triggers.Store == nil || r == nil {
		return nil, fmt.Errorf("source automation is unavailable")
	}
	kind, id := r.Target()
	if kind != "app" || id == "" || !r.Enabled || r.ActorSubject != "cooker:trigger:"+r.ID || len(r.ActorGroups) != 0 || r.AppVersion < 0 || r.SourceVersion < 1 || !validHex(r.PlanHash, 64) || !validHex(r.ApprovedEnvelopeHash, 64) {
		return nil, fmt.Errorf("reviewed App rule is required")
	}
	current, err := a.Triggers.Store.GetRule(ctx, r.ID)
	if err != nil || !reflect.DeepEqual(current, r) {
		return nil, fmt.Errorf("App rule changed; reload and review it")
	}
	if ok, _ := TriggerRuleMatches(r, model.VerifiedGitEvent{Source: r.Source, FullRef: fullRef}); !ok {
		return nil, fmt.Errorf("ref is outside the reviewed rule")
	}
	app, err := a.Store.Apps.Get(ctx, id)
	if err != nil || app == nil || app.Version != r.AppVersion || app.EnvironmentID != r.EnvironmentID {
		return nil, fmt.Errorf("App changed after rule review")
	}
	conn, err := a.Connections.Store.Get(ctx, r.Source.ConnectionID)
	if err != nil || conn == nil || conn.Version != r.SourceVersion || !conn.Allows(app.GitHubRepo) {
		return nil, fmt.Errorf("source connection requires another review")
	}
	if app.EnvironmentID == "" {
		if r.EnvironmentVersion != 0 {
			return nil, fmt.Errorf("environment requires another review")
		}
	} else {
		if a.Store.Environments == nil {
			return nil, fmt.Errorf("environment is unavailable")
		}
		env, err := a.Store.Environments.Get(ctx, app.EnvironmentID)
		if err != nil || env == nil || env.Version != r.EnvironmentVersion {
			return nil, fmt.Errorf("environment requires another review")
		}
	}
	if app.BuildPlan == nil || app.BuildPlan.Kind != model.BuildPlanCompose {
		return nil, fmt.Errorf("reviewed Compose configuration is required")
	}
	// Preview normalizes these metadata fields before hashing. Reconstruct that
	// same shape using the saved repository ID without opening a credential or
	// contacting the provider; actual identity is verified again by prepareApp.
	configuration := store.CloneTrigger(app)
	configuration.BuildPlan.SourceConnectionID = conn.ID
	configuration.BuildPlan.SourceConnectionVersion = conn.Version
	configuration.BuildPlan.InstallationID = conn.InstallationID
	configuration.BuildPlan.SourceRepositoryID = r.Source.RepositoryID
	if automationPlanHash(configuration, r.EnvironmentVersion, conn.Version) != r.PlanHash {
		return nil, fmt.Errorf("deployment configuration requires another review")
	}
	if a.Authorize == nil || a.Authorize(ctx, r, app) != nil {
		return nil, fmt.Errorf("automation actor is not authorized")
	}
	return app, nil
}
