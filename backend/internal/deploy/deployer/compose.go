// compose.go — a Deployer that brings up a whole Docker Compose stack
// via `docker compose up -d`. This is the "stack mode" fallback for
// docker-host deploys, as opposed to the per-service `docker run` path
// in dockerrun.go. Selected when a deploy stage's DeployRuntime is
// "compose".
//
// SECURITY: same Docker-daemon caveat as dockerrun.go — opt-in, not for
// hardened production. See SECURITY.md.
package deployer

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// Compose deploys a stack with `docker compose -p <project> up -d`.
type Compose struct {
	// Bin overrides the docker binary path. Empty means "docker" on PATH.
	Bin string
	// Serialize stack mutation: parallel DAG branches can share networks/volumes.
	mu sync.Mutex
}

// NewCompose constructs a compose-up deployer.
func NewCompose() *Compose { return &Compose{} }

func (c *Compose) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "docker"
}

// Deploy runs `docker compose -p <name> -f <file> up -d --remove-orphans`.
// req.Name is the project name; req.ComposeFile is the path to the
// compose file (it must exist for the daemon's lifetime of the command).
func (c *Compose) Deploy(ctx context.Context, req Request) (Result, error) {
	if req.Kind != KindCompose {
		return Result{}, fmt.Errorf("%w: compose got kind %q", ErrUnavailable, req.Kind)
	}
	if req.ComposeFile == "" {
		return Result{}, fmt.Errorf("compose: ComposeFile is empty")
	}
	bin := c.bin()
	if _, err := exec.LookPath(bin); err != nil {
		return Result{}, fmt.Errorf("%w: docker CLI not found: %v", ErrUnavailable, err)
	}
	project := req.Name
	if project == "" {
		project = "cooker"
	}
	out := req.LogWriter
	if out == nil {
		out = io.Discard
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	base := []string{"compose", "-p", project, "-f", req.ComposeFile}
	if req.ComposeOverrideFile != "" {
		base = append(base, "-f", req.ComposeOverrideFile)
	}
	var result Result
	err := withRegistryDockerEnvironment(ctx, req, func(env []string) error {
		if req.RegistryAuth != nil {
			inspectArgs := append(append([]string(nil), base...), "config", "--images")
			if req.ComposeService != "" {
				inspectArgs = append(inspectArgs, req.ComposeService)
			}
			inspect := exec.CommandContext(ctx, bin, inspectArgs...)
			inspect.Env = env
			images := &boundedRuntimeImages{}
			inspect.Stdout = images
			if err := inspect.Run(); err != nil {
				return fmt.Errorf("reviewed runtime images are unavailable")
			}
			seen := map[string]bool{}
			for _, image := range strings.Fields(images.String()) {
				if seen[image] {
					continue
				}
				seen[image] = true
				if len(seen) > 256 {
					return fmt.Errorf("too many runtime images")
				}
				if err := prepareDockerRuntimeImage(ctx, bin, image, env, out); err != nil {
					return err
				}
			}
			if len(seen) == 0 {
				return fmt.Errorf("reviewed runtime images are unavailable")
			}
		}
		args := append([]string(nil), base...)
		args = append(args, "up", "-d")
		if req.ComposeService != "" {
			args = append(args, "--no-build", "--no-deps", "--wait", "--wait-timeout", "300", req.ComposeService)
		} else {
			args = append(args, "--remove-orphans")
		}
		logf(out, "Running: docker %v\n", args)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = env
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker compose up (project %s): %w", project, err)
		}
		logf(out, "Compose stack %s is up\n", project)
		result = Result{AppliedResources: []string{"compose/" + project}}
		return nil
	})
	return result, err
}

var _ Deployer = (*Compose)(nil)
