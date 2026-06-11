// Package stagerunner runs a user-supplied command or script inside an
// isolated container for Test and Custom pipeline stages. It mirrors the
// builder/pusher/deployer adapter pattern: a narrow Runner interface with
// pluggable backends (a Kubernetes Job, a docker CLI `docker run`, or a
// Noop for tests), selected at boot via COOKER_STAGE_RUNNER.
//
// SECURITY: a Test/Custom stage executes user-controlled code. It MUST
// NOT run on the Cooker server process. Every real backend confines the
// command to a throwaway container (Job pod / `docker run --rm`) so a
// malicious script is contained by whatever the operator's runtime
// enforces, not by Cooker's own privileges. See SECURITY.md
// "Test/Custom stage execution model".
package stagerunner

import (
	"context"
	"errors"
	"io"
)

// ErrUnavailable is returned when the configured runtime is not reachable
// or not configured (no kube client, docker CLI absent). Callers treat it
// as a configuration error, not a stage failure — same contract as
// builder.ErrUnavailable.
var ErrUnavailable = errors.New("stagerunner: unavailable")

// Request describes a single containerized stage execution.
type Request struct {
	// Image is the OCI image the command runs in (e.g. "node:20-alpine").
	// Required: a Test/Custom stage with no image is a config error.
	Image string
	// Command is an explicit argv. When set it is used verbatim as the
	// container's command. Mutually informative with Script: if Script is
	// set and Command is empty, the runner wraps the script in a shell.
	Command []string
	// Script is a shell snippet (Custom stages). When set and Command is
	// empty the runner executes `sh -c <script>` inside the container.
	Script string
	// Env are environment variables injected into the container. Values
	// are already resolved (secrets decrypted) by the executor before they
	// reach here.
	Env map[string]string
	// WorkDir optionally sets the container working directory. Empty uses
	// the image default.
	WorkDir string
	// LogWriter receives the container's combined stdout/stderr as it
	// runs. Nil discards the stream.
	LogWriter io.Writer
}

// Result reports the outcome of a stage execution. A non-nil error from
// Run means the runtime itself failed (couldn't start the container,
// lost the API connection); a started container that exits non-zero is
// reported as a nil error with ExitCode != 0, and the executor maps that
// to a failed stage.
type Result struct {
	// ExitCode is the container's exit status. 0 is success; the executor
	// fails the stage on any non-zero value.
	ExitCode int
}

// Runner executes a command/script inside an isolated container.
// Implementations must be safe for concurrent use by multiple goroutines.
type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
}

// shellCommand returns the argv a backend should run for req. An explicit
// Command wins; otherwise a non-empty Script is wrapped in `sh -c`. The
// result is empty when neither is set, which backends treat as "use the
// image's default entrypoint/cmd" (a Test stage that only asserts the
// image starts cleanly).
func shellCommand(req Request) []string {
	if len(req.Command) > 0 {
		return req.Command
	}
	if req.Script != "" {
		return []string{"sh", "-c", req.Script}
	}
	return nil
}

// validateRequest enforces the one invariant every backend shares: an
// image is required. Command/Script are optional (an image may define its
// own entrypoint).
func validateRequest(req Request) error {
	if req.Image == "" {
		return errors.New("stagerunner: Image is required")
	}
	return nil
}
