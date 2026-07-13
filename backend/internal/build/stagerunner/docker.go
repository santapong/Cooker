package stagerunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
)

// DockerRun executes a stage command by shelling out to `docker run
// --rm`, which talks to the local Docker daemon over its socket. Intended
// for development and single-host installs where a Kubernetes Job would be
// overkill. The container is removed on exit (--rm); a non-zero exit code
// surfaces via the *exec.ExitError and is reported as Result.ExitCode.
//
// SECURITY: the command is user-supplied (Custom stage Script / Test stage
// Command). `docker run` confines it to the image's container, but the
// Docker daemon itself is a privileged surface — operators who cannot
// accept that should select the "kube" runner instead, the same way the
// docker builder is discouraged in production (config.Validate). No host
// paths are bind-mounted here.
type DockerRun struct {
	// Bin overrides the docker binary path. Empty means "docker" on PATH.
	Bin string
}

// NewDockerRun returns a DockerRun with default configuration.
func NewDockerRun() *DockerRun { return &DockerRun{} }

// Run starts `docker run --rm <env> -w <workdir> <image> <cmd>` and
// streams its combined output to req.LogWriter.
func (d *DockerRun) Run(ctx context.Context, req Request) (Result, error) {
	if err := validateRequest(req); err != nil {
		return Result{}, err
	}
	bin := d.Bin
	if bin == "" {
		bin = "docker"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return Result{}, fmt.Errorf("%w: docker CLI not found: %v", ErrUnavailable, err)
	}

	args := []string{"run", "--rm"}
	// Deterministic env ordering keeps the argv stable for tests and logs.
	for _, k := range sortedKeys(req.Env) {
		args = append(args, "-e", k+"="+req.Env[k])
	}
	if req.WorkDir != "" {
		args = append(args, "-w", req.WorkDir)
	}
	args = append(args, req.Image)
	args = append(args, shellCommand(req)...)

	cmd := exec.CommandContext(ctx, bin, args...)
	out := req.LogWriter
	if out == nil {
		out = io.Discard
	}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		// A non-zero container exit is a *stage* failure, not a runtime
		// failure: report the code with a nil error so the executor marks
		// the stage failed (rather than treating it as "runner broken").
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return Result{ExitCode: exitErr.ExitCode()}, nil
		}
		// LookPath passed but the daemon may be down / image unpullable:
		// that's a runtime-unavailable condition.
		return Result{}, fmt.Errorf("%w: docker run: %v", ErrUnavailable, err)
	}
	return Result{ExitCode: 0}, nil
}

// sortedKeys returns the map keys in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var _ Runner = (*DockerRun)(nil)
