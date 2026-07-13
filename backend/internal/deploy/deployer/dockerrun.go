// dockerrun.go — a Deployer that runs a single service as a container
// on a Docker host via the `docker` CLI. It backs the per-service
// deploy stage of a compose "deployment DAG" when the App targets
// docker-host (DeployTargetDockerHost). The compose runtime (whole-
// stack `docker compose up`) lives in compose.go.
//
// SECURITY: this talks to the Docker daemon (DOCKER_HOST, default the
// host socket), which is a root-equivalent capability. It is gated the
// same way as the docker builder/pusher — opt-in, not for hardened
// production. See SECURITY.md.
package deployer

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
)

// DockerRun runs one container per Deploy call via `docker run`.
type DockerRun struct {
	// Bin overrides the docker binary path. Empty means "docker" on PATH.
	Bin string
}

// NewDockerRun constructs a docker-run deployer.
func NewDockerRun() *DockerRun { return &DockerRun{} }

func (d *DockerRun) bin() string {
	if d.Bin != "" {
		return d.Bin
	}
	return "docker"
}

// Deploy runs (idempotently) the request's image as a detached,
// restart-always container named req.Name. An existing container with
// the same name is removed first so re-deploys replace it.
func (d *DockerRun) Deploy(ctx context.Context, req Request) (Result, error) {
	if req.Kind != KindDockerRun {
		return Result{}, fmt.Errorf("%w: dockerrun got kind %q", ErrUnavailable, req.Kind)
	}
	if req.Image == "" {
		return Result{}, fmt.Errorf("dockerrun: image is empty")
	}
	bin := d.bin()
	if _, err := exec.LookPath(bin); err != nil {
		return Result{}, fmt.Errorf("%w: docker CLI not found: %v", ErrUnavailable, err)
	}
	name := req.Name
	if name == "" {
		name = "cooker-service"
	}
	out := req.LogWriter
	if out == nil {
		out = io.Discard
	}

	// Replace any existing container of the same name (best-effort).
	// L dockerrun.go:63: log the error (e.g. "No such container") to
	// the writer so operators can see it in the run log rather than
	// the error being fully discarded.
	logf(out, "Removing existing container %s (if any)\n", name)
	if rmOut, rmErr := exec.CommandContext(ctx, bin, "rm", "-f", name).CombinedOutput(); rmErr != nil {
		logf(out, "docker rm -f %s: %v (output: %s)\n", name, rmErr, strings.TrimSpace(string(rmOut)))
	}

	args := dockerRunArgs(name, req.Image, req.Env, req.Ports, req.Resources, req.Labels, req.Network)
	logf(out, "Running: docker %s\n", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("docker run %s: %w", name, err)
	}
	logf(out, "Started container %s from %s\n", name, req.Image)
	return Result{AppliedResources: []string{"container/" + name}}, nil
}

// dockerRunArgs builds the argv for `docker run -d --restart=always
// --name N [--memory M] [--cpus C] [--network N] -p P -e K=V
// --label K=V ... IMAGE`. argv form (not a shell string) means values
// are passed verbatim with no shell interpretation — no quoting/
// injection concern. Ports, env, and labels are emitted in stable
// sorted order for deterministic, testable output.
func dockerRunArgs(name, image string, env map[string]string, ports []string, res *ResourceLimits, labels map[string]string, network string) []string {
	args := []string{"run", "-d", "--restart=always", "--name", name}

	if network != "" {
		args = append(args, "--network", network)
	}

	if res != nil {
		if res.Memory != "" {
			args = append(args, "--memory", res.Memory)
		}
		if res.CPUs != "" {
			args = append(args, "--cpus", res.CPUs)
		}
	}

	sortedPorts := append([]string(nil), ports...)
	sort.Strings(sortedPorts)
	for _, p := range sortedPorts {
		if p == "" {
			continue
		}
		// Normalize a bare container port "80" to "80:80" so it's published.
		if !strings.Contains(p, ":") {
			p = p + ":" + p
		}
		args = append(args, "-p", p)
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+env[k])
	}

	labelKeys := make([]string, 0, len(labels))
	for k := range labels {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)
	for _, k := range labelKeys {
		args = append(args, "--label", k+"="+labels[k])
	}

	args = append(args, image)
	return args
}

var _ Deployer = (*DockerRun)(nil)
