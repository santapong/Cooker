package stagerunner

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNoop_ReportsSuccess(t *testing.T) {
	var buf bytes.Buffer
	res, err := Noop{}.Run(context.Background(), Request{
		Image: "alpine", Script: "echo hi", LogWriter: &buf,
	})
	if err != nil {
		t.Fatalf("noop run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("noop exit = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(buf.String(), "alpine") {
		t.Errorf("noop should echo the image, got %q", buf.String())
	}
}

func TestValidateRequest_ImageRequired(t *testing.T) {
	_, err := (&DockerRun{}).Run(context.Background(), Request{Script: "echo hi"})
	if err == nil {
		t.Fatal("expected error when Image is empty")
	}
	if !strings.Contains(err.Error(), "Image is required") {
		t.Errorf("error = %v, want it to mention Image is required", err)
	}
}

func TestShellCommand(t *testing.T) {
	// Explicit command wins.
	if got := shellCommand(Request{Command: []string{"go", "test"}, Script: "ignored"}); strings.Join(got, " ") != "go test" {
		t.Errorf("command precedence: got %v", got)
	}
	// Script wraps in sh -c.
	got := shellCommand(Request{Script: "echo hi"})
	if len(got) != 3 || got[0] != "sh" || got[1] != "-c" || got[2] != "echo hi" {
		t.Errorf("script wrap: got %v, want [sh -c echo hi]", got)
	}
	// Neither: empty (use image default).
	if got := shellCommand(Request{}); got != nil {
		t.Errorf("empty: got %v, want nil", got)
	}
}

func TestDockerRun_SuccessWithFakeBin(t *testing.T) {
	// "true" exits 0 for any argv, so Run succeeds without a real daemon.
	var buf bytes.Buffer
	res, err := (&DockerRun{Bin: "true"}).Run(context.Background(), Request{
		Image: "alpine:3.20", Script: "echo hi",
		Env:       map[string]string{"FOO": "bar"},
		LogWriter: &buf,
	})
	if err != nil {
		t.Fatalf("docker run (true): %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", res.ExitCode)
	}
}

func TestDockerRun_NonZeroExitReportsCode(t *testing.T) {
	// "false" exits 1; the runner must report ExitCode=1 with a nil error
	// (a stage failure, not a runtime failure).
	res, err := (&DockerRun{Bin: "false"}).Run(context.Background(), Request{
		Image: "alpine:3.20", Command: []string{"whatever"},
	})
	if err != nil {
		t.Fatalf("non-zero exit should be a nil error, got %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("exit = %d, want 1", res.ExitCode)
	}
}

func TestDockerRun_MissingBinUnavailable(t *testing.T) {
	_, err := (&DockerRun{Bin: "definitely-not-a-real-binary-xyz"}).Run(context.Background(), Request{
		Image: "alpine",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing docker bin: err = %v, want ErrUnavailable chain", err)
	}
}
