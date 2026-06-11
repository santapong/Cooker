package stagerunner

import (
	"context"
	"fmt"
)

// Noop is the default runner: it does not start a container. It echoes
// what it would have run to the log writer and reports success (exit 0)
// so a pipeline with Test/Custom stages is exercisable in dev and in
// tests without a container runtime. Production installs select "docker"
// or "kube" via COOKER_STAGE_RUNNER.
type Noop struct{}

// Run logs the intended command and returns a clean exit.
func (Noop) Run(_ context.Context, req Request) (Result, error) {
	if req.LogWriter != nil {
		cmd := shellCommand(req)
		fmt.Fprintf(req.LogWriter, "noop: would run %q in %s (cmd=%v)\n", req.Script, req.Image, cmd)
	}
	return Result{ExitCode: 0}, nil
}

var _ Runner = Noop{}
