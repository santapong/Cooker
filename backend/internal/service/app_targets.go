package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/santapong/cooker/internal/deploy/deploytarget"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
)

// DeploymentViewPipeline is a read-only graph for persistence/UI. Execution
// uses the original in-memory plan; secret values never enter pipeline JSONB.
func DeploymentViewPipeline(p *model.Pipeline) *model.Pipeline {
	data, _ := json.Marshal(p)
	var view model.Pipeline
	_ = json.Unmarshal(data, &view)
	for i := range view.Stages {
		c := &view.Stages[i].Config
		c.ReviewOnly = true
		for k := range c.Env {
			c.Env[k] = "[configured]"
		}
		for k := range c.BuildArgs {
			c.BuildArgs[k] = "[configured]"
		}
		if c.ManifestPath != "" {
			c.ManifestPath = "[resolved during app deployment]"
		}
		c.Command = nil
		if c.HealthCheck != nil {
			c.HealthCheck.Command = []string{"[configured]"}
		}
	}
	return &view
}

var prefixPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
var variablePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var namespacePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func AppPrefix(app *model.App) string {
	if app.DeployTarget.Prefix != "" {
		return app.DeployTarget.Prefix
	}
	return sanitize(app.Name)
}

func RuntimeServiceName(app *model.App, service string) string {
	return AppPrefix(app) + "-" + sanitize(service)
}

// ValidateAppDeployment validates persisted configuration without accessing
// credentials, source files or the network. Capability checks are separate.
func ValidateAppDeployment(app *model.App) error {
	if app.RegistryRef != "" {
		if _, err := reference.ParseNormalizedNamed(app.RegistryRef + "/cooker-preview:review"); err != nil {
			return fmt.Errorf("image registry must be a host and optional repository path, without credentials or a tag")
		}
	}
	if app.DeployTarget.Namespace != "" && !namespacePattern.MatchString(app.DeployTarget.Namespace) {
		return fmt.Errorf("invalid Kubernetes namespace")
	}
	if app.BuildPlan != nil && app.BuildPlan.Kind != model.BuildPlanCompose && len(app.DeployTarget.ExternalServices) > 0 {
		return fmt.Errorf("external database substitution requires a Compose plan")
	}
	if app.DeployTarget.Namespace != "" && app.DeployTarget.Kind != model.DeployTargetKubernetes {
		return fmt.Errorf("namespace is only supported for Kubernetes")
	}
	if app.DeployTarget.Region != "" || app.DeployTarget.Service != "" {
		return fmt.Errorf("cloud region and workload identity use the configured target and deployment prefix")
	}
	if app.DeployTarget.Prefix != "" && !prefixPattern.MatchString(app.DeployTarget.Prefix) {
		return fmt.Errorf("prefix must be 1–40 lowercase letters, digits or hyphens, starting and ending with a letter or digit")
	}
	if app.DeployTarget.Kind == model.DeployTargetCloudRun {
		prefix := AppPrefix(app)
		if !namespacePattern.MatchString(prefix) || len(prefix) >= 50 || prefix[0] < 'a' || prefix[0] > 'z' {
			return fmt.Errorf("the Cloud Run prefix must start with a letter and contain fewer than 50 characters")
		}
	}
	if app.BuildPlan != nil {
		p := app.BuildPlan
		if p.Kind != model.BuildPlanCompose && p.Kind != model.BuildPlanDockerfile && p.Kind != model.BuildPlanBuildpack {
			return fmt.Errorf("unknown build plan")
		}
		if p.Commit != "" && !github.ValidCommit(p.Commit) {
			return fmt.Errorf("commit must be a full 40-character Git SHA")
		}
		if p.InstallationID < 0 {
			return fmt.Errorf("invalid GitHub installation")
		}
		if len(p.Files) > 8 || len(p.Profiles) > 32 || len(p.Variables) > 128 {
			return fmt.Errorf("too many Compose inputs")
		}
		for _, path := range append(append([]string{}, p.Files...), p.Path) {
			if path != "" && !safeRelativePath(path) {
				return fmt.Errorf("compose and Dockerfile paths must stay inside the repository")
			}
		}
		for key, value := range p.Variables {
			if !variablePattern.MatchString(key) || len(value) > 8192 {
				return fmt.Errorf("invalid Compose variable %q", key)
			}
		}
		if p.Commit != "" && app.AutoDeploy {
			return fmt.Errorf("reviewed commits use manual deployment; inspect and save a new revision before deploying a branch update")
		}
	}
	seen := map[string]bool{}
	for _, b := range app.DeployTarget.ExternalServices {
		if b.Service == "" || seen[b.Service] {
			return fmt.Errorf("external service bindings must have unique service names")
		}
		seen[b.Service] = true
		if b.Provider != "gcp-cloud-sql" && b.Provider != "external" {
			return fmt.Errorf("unsupported external database provider")
		}
		if strings.TrimSpace(b.Resource) == "" || len(b.Resource) > 256 {
			return fmt.Errorf("external resource name is required")
		}
		if len(b.Consumers) == 0 || len(b.Environment) == 0 {
			return fmt.Errorf("external service %s needs consumers and environment key bindings", b.Service)
		}
		for target, source := range b.Environment {
			if !variablePattern.MatchString(target) || !variablePattern.MatchString(source) {
				return fmt.Errorf("external bindings must reference environment key names, not connection values")
			}
		}
		if app.EnvironmentID == "" {
			return fmt.Errorf("select an environment for the external database connection")
		}
	}
	return nil
}

type TargetCapability struct {
	Kind      model.DeployTargetKind `json:"kind"`
	Available bool                   `json:"available"`
	Reason    string                 `json:"reason,omitempty"`
}

func CheckAppTarget(app *model.App) error {
	switch app.DeployTarget.Kind {
	case model.DeployTargetDockerHost:
		if app.DeployTarget.HostID != "" {
			return fmt.Errorf("managed Docker host selection is not wired to app deployment; use the Cooker-local Docker runtime")
		}
		return nil
	case model.DeployTargetKubernetes:
		return nil
	case model.DeployTargetECS, model.DeployTargetCloudRun:
		t, err := deploytarget.Lookup(app.DeployTarget.Kind)
		if err != nil {
			return err
		}
		if v, ok := t.(interface{ Validate() error }); ok {
			return v.Validate()
		}
		return nil
	default:
		return fmt.Errorf("app deployment target %q is not supported", app.DeployTarget.Kind)
	}
}

func AppTargetCapabilities() []TargetCapability {
	var caps []TargetCapability
	for _, kind := range []model.DeployTargetKind{model.DeployTargetDockerHost, model.DeployTargetKubernetes, model.DeployTargetECS, model.DeployTargetCloudRun} {
		err := CheckAppTarget(&model.App{DeployTarget: model.DeployTarget{Kind: kind}})
		cap := TargetCapability{Kind: kind, Available: err == nil}
		if err != nil {
			cap.Reason = err.Error()
		}
		caps = append(caps, cap)
	}
	return caps
}

func (e *Executor) executeCloudDeploy(ctx context.Context, stage *model.Stage, writer io.Writer) error {
	target, err := deploytarget.Lookup(model.DeployTargetKind(stage.Config.DeployRuntime))
	if err != nil {
		return err
	}
	name := stage.Config.RuntimeName
	if name == "" {
		return fmt.Errorf("cloud deployment requires a scoped workload name")
	}
	spec := deploytarget.Spec{AppID: name, Image: stage.Config.Image, Env: stage.Config.Env, Replicas: 1, LogWriter: writer, Command: stage.Config.Command, Resources: stage.Config.Resources, HealthCheck: stage.Config.HealthCheck}
	for _, port := range stage.Config.ComposePorts {
		n := firstContainerPort([]string{port})
		if n > 0 {
			spec.Ports = append(spec.Ports, n)
		}
	}
	if err := target.Deploy(ctx, spec); err != nil {
		return err
	}
	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		status, err := target.Status(readyCtx, name)
		if err != nil {
			return fmt.Errorf("verify cloud deployment: %w", err)
		}
		ready := status.Healthy
		if ready {
			if verifier, ok := target.(interface {
				VerifyImage(context.Context, string, string) error
			}); ok {
				if err := verifier.VerifyImage(readyCtx, name, spec.Image); err != nil {
					ready = false // An older healthy revision may still be visible during rollout.
				}
			}
			if ready {
				fmt.Fprintf(writer, "Ready %s/%s\n", target.Kind(), name)
				return nil
			}
		}
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("workload %s did not become ready: %w", name, readyCtx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}
