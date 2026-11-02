package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type runtimeObservation struct {
	deployment model.AppDeploy
	stages     []model.Stage
}

func (r *RuntimeService) observe(ctx context.Context, app *model.App) (*runtimeObservation, error) {
	if app == nil || r.Store == nil || r.Store.AppDeploys == nil || r.Store.Pipelines == nil {
		return nil, fmt.Errorf("deployment inventory is unavailable")
	}
	latest, err := store.LatestRuntimeAttempts(ctx, r.Store.AppDeploys, []string{app.ID})
	deployment := latest[app.ID]
	if err != nil {
		return nil, fmt.Errorf("runtime deployment inventory is unavailable")
	}
	if deployment == nil {
		return nil, fmt.Errorf("no recorded deployment is available")
	}
	p, err := r.Store.Pipelines.Get(ctx, deployment.PipelineID)
	if err != nil {
		return nil, fmt.Errorf("recorded deployment graph is unavailable")
	}
	var stages []model.Stage
	for _, stage := range p.Stages {
		if stage.Type == model.StageTypeDeploy && stage.Config.RuntimeName != "" {
			stages = append(stages, stage)
		}
	}
	if len(stages) == 0 {
		return nil, fmt.Errorf("deployment has no observable services")
	}
	return &runtimeObservation{deployment: *deployment, stages: stages}, nil
}

func runtimeServiceKey(stage model.Stage) string {
	if stage.Config.ComposeServiceName != "" {
		return stage.Config.ComposeServiceName
	}
	return stage.ID
}

// LogEnvironment resolves only the deployment's environment generation; use
// StreamDeploymentLogs to bind that generation atomically to a log session.
// Looking up the
// current App alone would forget old secrets after an edit or rotation and
// expose them from a still-running container's historical logs. Until encrypted
// secret history exists, refuse that replay instead of claiming it is redacted.
func (r *RuntimeService) LogEnvironment(ctx context.Context, app *model.App, jobs jobqueue.Store, resolver *AppEnvResolver) (*ResolvedAppEnv, error) {
	if app == nil || r.Store == nil || r.Store.AppDeploys == nil {
		return nil, fmt.Errorf("service logs need a recorded durable deployment")
	}
	latest, err := store.LatestRuntimeAttempts(ctx, r.Store.AppDeploys, []string{app.ID})
	deployment := latest[app.ID]
	if err != nil {
		return nil, fmt.Errorf("runtime deployment inventory is unavailable")
	}
	if deployment == nil {
		return nil, fmt.Errorf("no recorded deployment is available")
	}
	return deploymentLogEnvironment(ctx, app.ID, deployment, jobs, resolver, !r.SecretGenerationBound)
}

func deploymentLogEnvironment(ctx context.Context, appID string, deployment *model.AppDeploy, jobs jobqueue.Store, resolver *AppEnvResolver, requirePlainOnly ...bool) (*ResolvedAppEnv, error) {
	if jobs == nil || deployment == nil || deployment.RunID == "" || deployment.AppID != appID {
		return nil, fmt.Errorf("service logs need a recorded durable deployment")
	}
	j, err := jobs.Get(ctx, "app-run:"+deployment.RunID)
	if err != nil {
		return nil, fmt.Errorf("service logs need a recorded durable deployment")
	}
	var plan AppDeployPlan
	if json.Unmarshal(j.Payload, &plan) != nil || j.Kind != JobKindAppDeploy || plan.App == nil || plan.App.ID != appID || plan.RunID != deployment.RunID {
		return nil, fmt.Errorf("deployment configuration could not be verified")
	}
	if plan.Secrets == nil && len(requirePlainOnly) > 0 && requirePlainOnly[0] && plan.App.EnvironmentID != "" {
		// A currently empty external secret list is not evidence that the old
		// deployment received no secrets. Only explicit service permissions can
		// prove that every injected key was from the versioned plain map.
		if !usesServiceEnvironment(plan.App.BuildPlan) || resolver == nil || resolver.Environments == nil {
			return nil, fmt.Errorf("unversioned secret backend requires explicit plain-only service permissions for log replay")
		}
		env, err := resolver.Environments.Get(ctx, plan.App.EnvironmentID)
		if err != nil || env.Version != plan.EnvironmentVersion {
			return nil, fmt.Errorf("recorded plain configuration generation is unavailable")
		}
		env.NormalisePlainVars()
		for _, keys := range plan.App.BuildPlan.ServiceEnvironment {
			for _, key := range keys {
				if _, plain := env.PlainVars[key]; !plain {
					return nil, fmt.Errorf("unversioned secret backend cannot replay a deployment that references secrets")
				}
			}
		}
		plan.Secrets = &EnvironmentSecretPlan{Version: 1, EnvironmentID: env.ID, EnvironmentVersion: env.Version, Mode: "none", Keys: []string{}}
	}
	ctx = WithEnvironmentVersion(ctx, plan.App.EnvironmentID, plan.EnvironmentVersion)
	if plan.Secrets != nil {
		ctx = WithAcceptedSecretPlan(ctx, plan.Secrets)
	}
	return resolver.ResolveDetailed(ctx, plan.App)
}

// workload reads one captured observation. Future App edits cannot replace its
// namespace, target, external bindings, or service identity.
func (o *runtimeObservation) workload(app *model.App, service string) (*model.App, string, error) {
	for _, stage := range o.stages {
		if runtimeServiceKey(stage) != service {
			continue
		}
		cp := *app
		cp.DeployTarget = model.DeployTarget{Namespace: stage.Config.Namespace}
		switch stage.Config.DeployRuntime {
		case "docker", "compose":
			cp.DeployTarget.Kind = model.DeployTargetDockerHost
		case "kubernetes":
			cp.DeployTarget.Kind = model.DeployTargetKubernetes
		case "ecs":
			cp.DeployTarget.Kind = model.DeployTargetECS
		case "cloud-run":
			cp.DeployTarget.Kind = model.DeployTargetCloudRun
		default:
			return nil, "", fmt.Errorf("recorded runtime is unsupported")
		}
		return &cp, stage.Config.RuntimeName, nil
	}
	return nil, "", fmt.Errorf("service is not part of the recorded deployment")
}

// Probe implements the periodic App health contract using recorded services.
// Merely running without a health check is not evidence of application health.
// Cloud probes remain outside the first Docker/Kubernetes acceptance scope.
func (r *RuntimeService) Probe(ctx context.Context, app *model.App) (model.AppHealth, string, string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	observed, err := r.observe(ctx, app)
	if err != nil {
		return model.AppHealthUnknown, err.Error(), ""
	}
	if observed.deployment.Status == model.RunStatusNeedsAttention {
		return model.AppHealthUnknown, "deployment outcome needs operator reconciliation", ""
	}
	healthy, degraded, failed, unknown := 0, 0, 0, 0
	for _, stage := range observed.stages {
		s, err := r.observedStatus(ctx, app, runtimeServiceKey(stage), observed)
		switch {
		case err != nil:
			unknown++
		case s.Healthy:
			healthy++
		case s.State == "exited" || s.State == "dead":
			failed++
		case s.State == "degraded" || s.State == "restarting" || s.State == "pending":
			degraded++
		default:
			unknown++
		}
	}
	message := fmt.Sprintf("%d/%d services report ready", healthy, len(observed.stages))
	if failed > 0 {
		return model.AppHealthFailed, message + "; a service has stopped", ""
	}
	if degraded > 0 {
		return model.AppHealthDegraded, message + "; readiness checks have not passed", ""
	}
	if unknown > 0 {
		return model.AppHealthUnknown, message + "; some services have no readiness observation", ""
	}
	if observed.deployment.Status != model.RunStatusSuccess {
		return model.AppHealthDegraded, message + "; latest deployment did not complete successfully", ""
	}
	return model.AppHealthHealthy, message, ""
}
