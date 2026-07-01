package service

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

// These tests are hermetic -- no real docker/kubectl is invoked. The
// RuntimeService.DockerBin/KubectlBin override fields exist specifically
// for this: pointing them at a nonexistent name reliably hits the
// exec.LookPath-fail branch, and pointing them at a real, always-present
// binary (echo) exercises the success path deterministically.
//
// Note (confirmed empirically, not asserted here): Go's os/exec.LookPath
// rejects directories outright ("is a directory") before cmd.Start() is
// ever reached. That means runStream's cmd.StdoutPipe()/cmd.Start() error
// branches are NOT independently reachable through the DockerBin/
// KubectlBin override seam -- only a deeper exec.Command-level seam
// could hit them, which isn't worth adding for two defensive-only lines
// that in practice are close to unreachable (StdoutPipe fails only if
// Stdout was already set or the process already started; Start fails
// only for a handful of OS-level exec failures). This file therefore
// covers the LookPath-fail branch and the success path, not those two.

func runtimeTestDockerApp() *model.App {
	return &model.App{ID: "app-1", Name: "svc", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
}

func runtimeTestKubeApp() *model.App {
	return &model.App{ID: "app-1", Name: "svc", DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes}}
}

func TestRuntimeService_Status_DockerBinaryNotFound(t *testing.T) {
	r := &RuntimeService{DockerBin: "cooker-definitely-not-a-real-binary-xyz"}
	st, err := r.Status(context.Background(), runtimeTestDockerApp(), "web")
	// Status() never returns a non-nil error in any branch: dockerStatus/
	// kubeStatus always return (RuntimeStatus, nil), mapping any failure
	// into .Message instead. Documented here as an invariant rather than
	// a coverage gap.
	if err != nil {
		t.Fatalf("Status must never error, got: %v", err)
	}
	if st.Message != "docker CLI not found" {
		t.Errorf("Message = %q, want %q", st.Message, "docker CLI not found")
	}
	if st.Runtime != "docker" {
		t.Errorf("Runtime = %q, want docker", st.Runtime)
	}
}

func TestRuntimeService_Status_KubectlBinaryNotFound(t *testing.T) {
	r := &RuntimeService{KubectlBin: "cooker-definitely-not-a-real-binary-xyz"}
	st, err := r.Status(context.Background(), runtimeTestKubeApp(), "web")
	if err != nil {
		t.Fatalf("Status must never error, got: %v", err)
	}
	if st.Message != "kubectl not found" {
		t.Errorf("Message = %q, want %q", st.Message, "kubectl not found")
	}
	if st.Runtime != "kubernetes" {
		t.Errorf("Runtime = %q, want kubernetes", st.Runtime)
	}
}

func TestRuntimeService_Status_DockerInspectFails(t *testing.T) {
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Skip("no `false` binary on PATH, skipping")
	}
	// `false` exists (passes LookPath) but any `docker inspect ...`-shaped
	// invocation of it exits nonzero, so exec.Command.Output() errors --
	// exercising dockerStatus's inspect-failure branch, which still
	// returns a nil Go error (message-only).
	r := &RuntimeService{DockerBin: falseBin}
	st, err := r.Status(context.Background(), runtimeTestDockerApp(), "web")
	if err != nil {
		t.Fatalf("Status must never error, got: %v", err)
	}
	if st.State != "unknown" || st.Message != "not found" {
		t.Errorf("got State=%q Message=%q, want unknown/\"not found\"", st.State, st.Message)
	}
}

func TestRuntimeService_Status_KubeStatusMalformed(t *testing.T) {
	echoBin, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("no `echo` binary on PATH, skipping")
	}
	// `echo` just echoes its args back -- kubeStatus's jsonpath fields
	// won't contain a "ready/total" pair, so it should leave State as
	// "unknown" via the defensive fallback rather than misreading a
	// stray token as the state.
	r := &RuntimeService{KubectlBin: echoBin}
	st, err := r.Status(context.Background(), runtimeTestKubeApp(), "web")
	if err != nil {
		t.Fatalf("Status must never error, got: %v", err)
	}
	if st.State != "unknown" {
		t.Errorf("State = %q, want unknown (malformed jsonpath output)", st.State)
	}
}

func TestRuntimeService_Logs_BinaryNotFound(t *testing.T) {
	r := &RuntimeService{DockerBin: "cooker-definitely-not-a-real-binary-xyz"}
	var out bytes.Buffer
	err := r.Logs(context.Background(), runtimeTestDockerApp(), "web", &out)
	if err != nil {
		t.Fatalf("Logs on a missing binary should return nil (message written to out), got: %v", err)
	}
	if !strings.Contains(out.String(), "not found") {
		t.Errorf("out = %q, want a \"not found\" message", out.String())
	}
}

func TestRuntimeService_Logs_StreamsSuccessfully(t *testing.T) {
	echoBin, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("no `echo` binary on PATH, skipping")
	}
	r := &RuntimeService{DockerBin: echoBin}
	var out bytes.Buffer
	// Logs always appends "logs --follow --tail 200 <name>" as args for
	// the docker path; echo just prints them back, giving a
	// deterministic single line to scan.
	err = r.Logs(context.Background(), runtimeTestDockerApp(), "web", &out)
	if err != nil {
		t.Fatalf("Logs: unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "web") {
		t.Errorf("out = %q, want it to contain the echoed args (incl. service name)", out.String())
	}
}
