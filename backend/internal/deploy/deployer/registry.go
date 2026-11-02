package deployer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/santapong/cooker/internal/build/registryauth"
)

type boundedRuntimeImages struct{ bytes.Buffer }

func (b *boundedRuntimeImages) Write(p []byte) (int, error) {
	if len(p) > (256<<10)-b.Len() {
		return 0, fmt.Errorf("too many runtime image bytes")
	}
	return b.Buffer.Write(p)
}

func withRegistryDockerEnvironment(ctx context.Context, req Request, use func([]string) error) error {
	if req.RegistryAuth == nil {
		if len(req.RegistryHosts) != 0 {
			return registryauth.ErrUnavailable
		}
		return use(nil) // compatibility for unconfigured legacy adapters only
	}
	return registryauth.WithDockerConfig(ctx, req.RegistryAuth, req.RegistryHosts, func(dir string) error {
		return use(registryauth.DockerEnvironment(dir))
	})
}

// Verify/pull before replacing a workload. Locally built images may already
// exist without a registry; a missing image must use only accepted credentials.
func prepareDockerRuntimeImage(ctx context.Context, bin, image string, env []string, out io.Writer) error {
	if _, err := name.ParseReference(image); err != nil {
		return fmt.Errorf("invalid runtime image reference")
	}
	inspect := exec.CommandContext(ctx, bin, "image", "inspect", image)
	inspect.Env = env
	if err := inspect.Run(); err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	pull := exec.CommandContext(ctx, bin, "pull", image)
	pull.Env, pull.Stdout, pull.Stderr = env, out, out
	if err := pull.Run(); err != nil {
		return fmt.Errorf("runtime image is unavailable: %w", err)
	}
	return nil
}
