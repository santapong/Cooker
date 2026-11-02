package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/santapong/cooker/internal/build/buildplan"
	"github.com/santapong/cooker/internal/build/registryauth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
)

// AppImageResolution is server-owned queue evidence, never an editable App
// field. The original reference binds each digest to the reviewed Compose input.
// A multi-platform image retains the index digest, not the admission host's arch.
type AppImageResolution struct {
	Version  int                    `json:"version"`
	Kind     model.BuildPlanKind    `json:"kind"`
	Services map[string]AppImagePin `json:"services"`
}
type AppImagePin struct {
	Requested string `json:"requested"`
	Image     string `json:"image"`
}
type ImageResolver func(context.Context, string) (string, error)
type appImageResolutionKey struct{}

var imageDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// RegistryImageResolver uses only the current request's configured registry
// identity. Anonymous means anonymous; host Docker login is never consulted.
func RegistryImageResolver(auth registryauth.Resolver) ImageResolver {
	return func(ctx context.Context, image string) (string, error) {
		ref, err := name.ParseReference(image)
		if err != nil {
			return "", fmt.Errorf("invalid container image reference")
		}
		// An existing content digest cannot move. Validate it locally, retaining
		// offline/cached deployments; availability is checked by the runtime.
		if _, ok := ref.(name.Digest); ok {
			return checkedImagePin(image, AppImagePin{Requested: ref.Name(), Image: ref.Name()})
		}
		identity := authn.Anonymous
		if auth != nil {
			identity, err = auth(ctx, ref.Context().RegistryStr())
			if err != nil || identity == nil {
				return "", fmt.Errorf("registry credential is unavailable")
			}
		}
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		desc, err := remote.Head(ref, remote.WithContext(requestCtx), remote.WithAuth(identity), remote.WithUserAgent("cooker/image-admission"))
		if err != nil {
			// Registry response bodies may contain credential or infrastructure data.
			return "", fmt.Errorf("cannot resolve container image; verify registry access and image availability")
		}
		if (!desc.MediaType.IsImage() && !desc.MediaType.IsIndex()) || desc.Digest.Algorithm != "sha256" {
			return "", fmt.Errorf("registry did not return a supported immutable image")
		}
		pinned := ref.Context().Digest(desc.Digest.String()).Name()
		if _, err := checkedImagePin(image, AppImagePin{Requested: ref.Name(), Image: pinned}); err != nil {
			return "", err
		}
		return pinned, nil
	}
}

// PrepareImages inspects the accepted source/config generation before a durable
// job exists. The source copy and resolved secrets are ephemeral; only image
// references and the effective build plan enter the queue payload.
func (d *AppDeployer) PrepareImages(ctx context.Context, plan *AppDeployPlan, resolve ImageResolver) (_ *AppImageResolution, resultErr error) {
	if plan == nil || plan.App == nil || !github.ValidCommit(plan.Commit) {
		return nil, fmt.Errorf("immutable source plan is required for image resolution")
	}
	if plan.Registry != nil {
		ctx = WithAcceptedRegistryAuthority(ctx, model.RegistrySubject{Kind: "app", ID: plan.App.ID}, plan.Registry)
	} else if d != nil && d.Executor != nil && d.Executor.registryAuthority != nil {
		return nil, ErrRegistryAuthorityRequired
	}
	var registryErr error
	ctx, registryErr = d.prepareRegistryContext(ctx, plan.App)
	if registryErr != nil {
		return nil, registryErr
	}
	ctx, cancel := context.WithTimeout(WithAcceptedSecretPlan(WithEnvironmentVersion(ctx, plan.App.EnvironmentID, plan.EnvironmentVersion), plan.Secrets), 2*time.Minute)
	defer cancel()
	env, err := d.EnvResolver.ResolveDetailed(ctx, plan.App)
	if err != nil {
		return nil, err
	}
	ctx = WithLogSecrets(ctx, env.SecretValues)
	if err := validateDockerfileBuildValues(ctx, plan.App); err != nil {
		return nil, err
	}
	defer func() { resultErr = redactLogError(ctx, resultErr) }()
	opts := github.CloneOptions{Repo: plan.App.GitHubRepo, Branch: plan.App.Branch, Commit: plan.Commit, Depth: 1, LogWriter: io.Discard}
	if plan.App.BuildPlan != nil {
		opts.InstallationID = plan.App.BuildPlan.InstallationID
	}
	var root string
	if d.SourceCheckout != nil && plan.App.BuildPlan != nil && plan.App.BuildPlan.SourceConnectionID != "" {
		root, err = d.SourceCheckout(ctx, plan.App, plan.Commit, io.Discard)
	} else {
		root, err = d.clone(ctx, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve source for image resolution")
	}
	defer os.RemoveAll(root)
	commit, err := github.HeadCommit(ctx, root)
	if err != nil || commit != plan.Commit {
		return nil, fmt.Errorf("image resolution checkout does not match accepted commit")
	}
	if plan.App.BuildPlan == nil {
		plan.App.BuildPlan = buildplan.Detect(root)
	}
	pins := &AppImageResolution{Version: 1, Kind: plan.App.BuildPlan.Kind, Services: map[string]AppImagePin{}}
	if pins.Kind != model.BuildPlanCompose {
		if pins.Kind != model.BuildPlanDockerfile {
			return nil, fmt.Errorf("source build kind is unsupported; select a Dockerfile or Compose plan")
		}
		path := plan.App.BuildPlan.Path
		if path == "" {
			path = "Dockerfile"
		}
		if _, err := repositoryPath(root, root, path); err != nil {
			return nil, err
		}
		if d.CheckExecution != nil {
			if err := d.CheckExecution(plan.App, true); err != nil {
				return nil, err
			}
		}
		if _, err := d.EnvResolver.ResolveDetailed(ctx, plan.App); err != nil {
			return nil, err
		}
		return pins, nil
	}
	resolved, err := loadRepositoryCompose(ctx, root, plan.App, env.Values)
	if err != nil {
		return nil, err
	}
	if len(resolved.Diagnostics) > 0 {
		return nil, fmt.Errorf("deployment configuration requires review before image resolution")
	}
	hasBuild := false
	for _, svc := range resolved.Graph.Services {
		if svc.External {
			continue
		}
		if svc.Build != nil {
			hasBuild = true
			continue
		}
		if resolve == nil {
			return nil, fmt.Errorf("image digest resolver is unavailable")
		}
		if RedactLogText(ctx, svc.Image) != svc.Image {
			return nil, fmt.Errorf("service %s image reference contains a secret", svc.Name)
		}
		image, err := resolve(ctx, svc.Image)
		if err != nil {
			return nil, fmt.Errorf("resolve image for service %s: %w", svc.Name, err)
		}
		ref, err := name.ParseReference(svc.Image)
		if err != nil {
			return nil, fmt.Errorf("invalid image reference for service %s", svc.Name)
		}
		pin := AppImagePin{Requested: ref.Name(), Image: image}
		if _, err := checkedImagePin(svc.Image, pin); err != nil {
			return nil, fmt.Errorf("resolve image for service %s: %w", svc.Name, err)
		}
		pins.Services[svc.Name] = pin
	}
	if d.CheckExecution != nil {
		if err := d.CheckExecution(plan.App, hasBuild); err != nil {
			return nil, err
		}
	}
	// Detect a rotation/edit while clone/registry requests were in flight.
	if _, err := d.EnvResolver.ResolveDetailed(ctx, plan.App); err != nil {
		return nil, err
	}
	return pins, nil
}

func checkedImagePin(image string, pin AppImagePin) (string, error) {
	ref, err := name.ParseReference(image)
	if err != nil || ref.Name() != pin.Requested {
		return "", fmt.Errorf("image input differs from its accepted resolution")
	}
	pinned, err := name.NewDigest(pin.Image)
	if err != nil || !imageDigestPattern.MatchString(pinned.DigestStr()) || pinned.Context().Name() != ref.Context().Name() {
		return "", fmt.Errorf("image resolution is not a digest of the requested repository")
	}
	if original, ok := ref.(name.Digest); ok && original.DigestStr() != pinned.DigestStr() {
		return "", fmt.Errorf("image resolution changed an already pinned digest")
	}
	return pinned.Name(), nil
}

func applyAppImageResolution(ctx context.Context, kind model.BuildPlanKind, resolved *resolvedCompose) error {
	pins, enabled := ctx.Value(appImageResolutionKey{}).(*AppImageResolution)
	if !enabled { // Legacy direct calls and explicitly injected test queues.
		return nil
	}
	if pins == nil || pins.Version != 1 || pins.Kind != kind || (kind != model.BuildPlanCompose && len(pins.Services) != 0) {
		return fmt.Errorf("accepted image resolution is missing or invalid; start a new deployment")
	}
	if kind != model.BuildPlanCompose {
		return nil
	}
	count := 0
	for i := range resolved.Graph.Services {
		svc := &resolved.Graph.Services[i]
		if svc.External || svc.Build != nil {
			continue
		}
		pin, ok := pins.Services[svc.Name]
		if !ok {
			return fmt.Errorf("service %s has no accepted image digest", svc.Name)
		}
		image, err := checkedImagePin(svc.Image, pin)
		if err != nil {
			return fmt.Errorf("service %s: %w", svc.Name, err)
		}
		svc.Image = image
		projectService := resolved.Project.Services[svc.Name]
		projectService.Image = image
		resolved.Project.Services[svc.Name] = projectService
		count++
	}
	if count != len(pins.Services) {
		return fmt.Errorf("accepted image resolution does not match deployment services")
	}
	return nil
}
