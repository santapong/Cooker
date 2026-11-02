package builder

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerBuildUsesContextRelativeDockerfileAndTarget(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "docker-test")
	output := filepath.Join(root, "arguments")
	t.Setenv("COOKER_TEST_ARGUMENTS", output)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = build ]; then printf '%s\\n' \"$@\" > \"$COOKER_TEST_ARGUMENTS\"; else printf 'sha256:fake\\n'; fi\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := (&DockerSock{Bin: bin}).Build(context.Background(), Request{ContextDir: "/repo/api", Dockerfile: "../docker/api.Dockerfile", Target: "runtime", Tags: []string{"registry.example.com/shop-api:1"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(output)
	if !strings.Contains(string(data), "-f\n/repo/docker/api.Dockerfile") || !strings.Contains(string(data), "--target\nruntime") {
		t.Fatalf("incorrect build arguments: %s", data)
	}
	contextDir := filepath.Join(root, "api")
	dockerfileDir := filepath.Join(root, "docker")
	for _, directory := range []string{contextDir, dockerfileDir} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		filepath.Join(contextDir, "context-marker"):    "build context",
		filepath.Join(dockerfileDir, "api.Dockerfile"): "FROM scratch",
	} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	opt, err := solveOptions(Request{ContextDir: contextDir, Dockerfile: "../docker/api.Dockerfile", Target: "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.FrontendAttrs["filename"] != "api.Dockerfile" || opt.FrontendAttrs["target"] != "runtime" {
		t.Fatalf("BuildKit context differs: %+v", opt)
	}
	for mount, file := range map[string]string{"context": "context-marker", "dockerfile": "api.Dockerfile"} {
		reader, err := opt.LocalMounts[mount].Open(file)
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil || len(contents) == 0 {
			t.Fatalf("mount %s did not provide the requested file: %v", mount, err)
		}
	}
}

func TestBuildKitMissingDockerfileDirectoryFailsBeforeConnecting(t *testing.T) {
	_, err := NewBuildKit("tcp://127.0.0.1:1").Build(context.Background(), Request{
		ContextDir: t.TempDir(), Dockerfile: "missing/Dockerfile",
	})
	if err == nil || !strings.Contains(err.Error(), "dockerfile filesystem") {
		t.Fatalf("expected local filesystem validation, got %v", err)
	}
}
