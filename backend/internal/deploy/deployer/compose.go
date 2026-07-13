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
)

// Compose deploys a stack with `docker compose -p <project> up -d`.
type Compose struct {
	// Bin overrides the docker binary path. Empty means "docker" on PATH.
	Bin string
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

	args := []string{"compose", "-p", project, "-f", req.ComposeFile, "up", "-d", "--remove-orphans"}
	logf(out, "Running: docker %v\n", args)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("docker compose up (project %s): %w", project, err)
	}
	logf(out, "Compose stack %s is up\n", project)
	return Result{AppliedResources: []string{"compose/" + project}}, nil
}

var _ Deployer = (*Compose)(nil)
