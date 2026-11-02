package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/model"
)

// Scoped registry credentials currently have Docker/Compose transport. Reject
// other deployment transports before checkout, secret resolution or build.
// Standalone image builds can still use App authority without a deploy stage.
func validateAppRegistryDeployment(app *model.App) error {
	if app == nil {
		return model.ErrInvalidRegistryAuthority
	}
	if app.DeployTarget.Kind != model.DeployTargetDockerHost {
		return fmt.Errorf("%w: scoped registry deployment currently supports Docker and Compose targets", deployer.ErrUnavailable)
	}
	return nil
}

func (d *AppDeployer) CheckRegistryDeployment(app *model.App) error {
	if d != nil && d.Executor != nil && d.Executor.registryAuthority != nil {
		return validateAppRegistryDeployment(app)
	}
	return nil
}

// AppRegistrySelection is explicit administrator-granted authority. A host name
// match or a deployment graph derived from this App does not add another grant.
func AppRegistrySelection(app *model.App) ([]string, error) {
	if app == nil || (model.RegistrySubject{Kind: "app", ID: app.ID}).Validate() != nil || strings.HasPrefix(app.ID, "vapp_") || strings.HasPrefix(app.ID, "vflow_") {
		return nil, model.ErrInvalidRegistryAuthority
	}
	var ids []string
	if app.BuildPlan != nil {
		ids = slices.Clone(app.BuildPlan.RegistryConnectionIDs)
	}
	if !model.ValidRegistryIDs(ids, 32) {
		return nil, model.ErrInvalidRegistryAuthority
	}
	sort.Strings(ids)
	return ids, nil
}

func PrepareAppRegistryAuthority(ctx context.Context, authority *RegistryAuthorityService, app *model.App) (*model.RegistryAuthorityPlan, error) {
	ids, err := AppRegistrySelection(app)
	if err != nil {
		return nil, err
	}
	if authority == nil {
		return nil, ErrRegistryAuthorityRequired
	}
	if err := ValidateRuntimeEnvironmentBinding(ctx, authority.Environments, app.EnvironmentID); err != nil {
		return nil, err
	}
	return authority.Prepare(ctx, model.RegistrySubject{Kind: "app", ID: app.ID}, ids, nil)
}

func validateAppRegistryPlan(app *model.App, plan *model.RegistryAuthorityPlan) error {
	ids, err := AppRegistrySelection(app)
	if err != nil || plan == nil || plan.Validate() != nil || plan.Subject != (model.RegistrySubject{Kind: "app", ID: app.ID}) || len(plan.Connections) != len(ids) {
		return model.ErrInvalidRegistryAuthority
	}
	for _, id := range ids {
		if _, ok := plan.Connections[id]; !ok {
			return model.ErrInvalidRegistryAuthority
		}
	}
	return nil
}

func (q *AppDeployQueue) prepareAppRegistry(ctx context.Context, app *model.App) (*model.RegistryAuthorityPlan, error) {
	ids, err := AppRegistrySelection(app)
	if err != nil {
		return nil, err
	}
	if q.RequireRegistryPins || q.RegistryAuthority != nil {
		if err := validateAppRegistryDeployment(app); err != nil {
			return nil, err
		}
	}
	if q.RegistryAuthority == nil {
		if q.RequireRegistryPins || len(ids) != 0 {
			return nil, ErrRegistryAuthorityRequired
		}
		return nil, nil // Explicitly unconfigured legacy fixture; no credential selection.
	}
	return PrepareAppRegistryAuthority(ctx, q.RegistryAuthority, app)
}

func (q *AppDeployQueue) validateAcceptedAppRegistry(ctx context.Context, plan *AppDeployPlan) error {
	if q == nil || plan == nil || plan.App == nil {
		return model.ErrInvalidRegistryAuthority
	}
	ids, err := AppRegistrySelection(plan.App)
	if err != nil {
		return err
	}
	executorConfigured := q.Deployer != nil && q.Deployer.Executor != nil && q.Deployer.Executor.registryAuthority != nil
	if q.RequireRegistryPins || q.RegistryAuthority != nil || executorConfigured || plan.Registry != nil {
		if err := validateAppRegistryDeployment(plan.App); err != nil {
			return err
		}
	}
	if plan.Registry == nil {
		if q.RequireRegistryPins || q.RegistryAuthority != nil || executorConfigured || len(ids) != 0 {
			return ErrRegistryAuthorityRequired
		}
		return nil
	}
	if q.RegistryAuthority == nil || !executorConfigured {
		return ErrRegistryAuthorityRequired
	}
	if err := validateAppRegistryPlan(plan.App, plan.Registry); err != nil {
		return err
	}
	return q.RegistryAuthority.ValidateCurrent(ctx, plan.Registry.Subject, plan.Registry)
}

// prepareRegistryContext runs before source checkout or runtime-secret reads.
// A queued caller always supplies immutable evidence; direct configured App
// calls prepare App authority, never grants for a synthesized Pipeline ID.
func (d *AppDeployer) prepareRegistryContext(ctx context.Context, app *model.App) (context.Context, error) {
	ids, err := AppRegistrySelection(app)
	if err != nil {
		return ctx, err
	}
	accepted, exists := ctx.Value(acceptedRegistryAuthorityKey{}).(acceptedRegistryAuthority)
	var authority *RegistryAuthorityService
	if d != nil && d.Executor != nil {
		authority = d.Executor.registryAuthority
	}
	if !exists {
		if authority == nil {
			if len(ids) != 0 {
				return ctx, ErrRegistryAuthorityRequired
			}
			return ctx, nil
		}
		plan, err := PrepareAppRegistryAuthority(ctx, authority, app)
		if err != nil {
			return ctx, err
		}
		ctx = WithAcceptedRegistryAuthority(ctx, model.RegistrySubject{Kind: "app", ID: app.ID}, plan)
		accepted, _ = ctx.Value(acceptedRegistryAuthorityKey{}).(acceptedRegistryAuthority)
	}
	if authority == nil || accepted.Invalid || accepted.Subject != (model.RegistrySubject{Kind: "app", ID: app.ID}) || validateAppRegistryPlan(app, accepted.Plan) != nil {
		return ctx, ErrRegistryAuthorityRequired
	}
	if err := authority.ValidateCurrent(ctx, accepted.Subject, accepted.Plan); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func cloneAppDeploymentInput(app *model.App) (*model.App, error) {
	if app == nil {
		return nil, model.ErrInvalidRegistryAuthority
	}
	data, err := json.Marshal(app.Redact())
	if err != nil {
		return nil, err
	}
	var snapshot model.App
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}
