package deployer

import (
	"context"
	"strings"
	"testing"
)

// dockerRunArgs must emit a deterministic, correctly-ordered argv with
// resource flags, sorted ports, and sorted env.
func TestDockerRunArgs(t *testing.T) {
	args := dockerRunArgs(
		"web",
		"reg/app-web:123",
		map[string]string{"B": "2", "A": "1"},
		[]string{"80", "8443:443"},
		&ResourceLimits{Memory: "512m", CPUs: "1.5"},
		nil, "",
	)
	got := strings.Join(args, " ")

	// Image must be last.
	if args[len(args)-1] != "reg/app-web:123" {
		t.Errorf("image must be last arg, got %q", args[len(args)-1])
	}
	// Resource flags present.
	if !strings.Contains(got, "--memory 512m") || !strings.Contains(got, "--cpus 1.5") {
		t.Errorf("missing resource flags: %s", got)
	}
	// Bare "80" normalized to "80:80"; "8443:443" kept verbatim.
	if !strings.Contains(got, "-p 80:80") {
		t.Errorf("bare port not normalized: %s", got)
	}
	if !strings.Contains(got, "-p 8443:443") {
		t.Errorf("explicit port mapping lost: %s", got)
	}
	// Env sorted (A before B).
	if strings.Index(got, "-e A=1") > strings.Index(got, "-e B=2") {
		t.Errorf("env not sorted: %s", got)
	}
	// Restart policy + detached.
	if !strings.HasPrefix(got, "run -d --restart=always --name web") {
		t.Errorf("unexpected prefix: %s", got)
	}
}

// No resources → no --memory/--cpus flags.
func TestDockerRunArgs_NoResources(t *testing.T) {
	args := dockerRunArgs("svc", "nginx", nil, nil, nil, nil, "")
	got := strings.Join(args, " ")
	if strings.Contains(got, "--memory") || strings.Contains(got, "--cpus") {
		t.Errorf("unexpected resource flags: %s", got)
	}
	if got != "run -d --restart=always --name svc nginx" {
		t.Errorf("got %q", got)
	}
}

// Labels are sorted and the network flag is emitted — the reverse-proxy
// integration depends on both (Traefik router labels + shared network).
func TestDockerRunArgs_LabelsAndNetwork(t *testing.T) {
	args := dockerRunArgs("web", "img:1", nil, nil, nil, map[string]string{
		"traefik.http.routers.web.rule": "Host(`web.apps.example.com`)",
		"traefik.enable":                "true",
	}, "cooker-proxy")
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--network cooker-proxy") {
		t.Errorf("missing network flag: %s", got)
	}
	if !strings.Contains(got, "--label traefik.enable=true") {
		t.Errorf("missing enable label: %s", got)
	}
	if strings.Index(got, "traefik.enable") > strings.Index(got, "traefik.http.routers") {
		t.Errorf("labels not sorted: %s", got)
	}
	if args[len(args)-1] != "img:1" {
		t.Errorf("image must be last arg, got %q", args[len(args)-1])
	}
}

// Wrong Kind is rejected (defensive — executor routes by DeployRuntime).
func TestDockerRun_WrongKind(t *testing.T) {
	_, err := (&DockerRun{}).Deploy(context.Background(), Request{Kind: KindManifest, Image: "x"})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestDockerRun_EmptyImage(t *testing.T) {
	_, err := (&DockerRun{}).Deploy(context.Background(), Request{Kind: KindDockerRun})
	if err == nil {
		t.Fatal("expected error for empty image")
	}
}

func TestCompose_WrongKindAndEmptyFile(t *testing.T) {
	if _, err := (&Compose{}).Deploy(context.Background(), Request{Kind: KindDockerRun}); err == nil {
		t.Error("expected error for wrong kind")
	}
	if _, err := (&Compose{}).Deploy(context.Background(), Request{Kind: KindCompose}); err == nil {
		t.Error("expected error for empty compose file")
	}
}
