package builder

import (
	"context"
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
	opt := solveOptions(Request{ContextDir: "/repo/api", Dockerfile: "../docker/api.Dockerfile", Target: "runtime"})
	if opt.LocalDirs["context"] != "/repo/api" || opt.LocalDirs["dockerfile"] != "/repo/docker" || opt.FrontendAttrs["filename"] != "api.Dockerfile" || opt.FrontendAttrs["target"] != "runtime" {
		t.Fatalf("BuildKit context differs: %+v", opt)
	}
}
