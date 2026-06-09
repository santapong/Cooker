package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
)

// fakeClone returns a cloneFn that materialises the given files
// (name → contents) into a fresh directory per call. The detector
// removes the directory itself, so each call mints its own.
func fakeClone(t *testing.T, files map[string]string) func(context.Context, github.CloneOptions) (string, error) {
	t.Helper()
	return func(_ context.Context, _ github.CloneOptions) (string, error) {
		dir, err := os.MkdirTemp("", "cooker-detect-test-*")
		if err != nil {
			return "", err
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				return "", err
			}
		}
		return dir, nil
	}
}

func TestAppDetector_DetectBuild(t *testing.T) {
	cases := []struct {
		name       string
		files      map[string]string
		wantKind   model.BuildPlanKind
		wantRecipe string
	}{
		{"dockerfile + go", map[string]string{"Dockerfile": "FROM scratch", "go.mod": "module x"}, model.BuildPlanDockerfile, "go"},
		{"compose + node", map[string]string{"docker-compose.yml": "services: {}", "package.json": "{}"}, model.BuildPlanCompose, "node-static"},
		{"bare repo", map[string]string{"README.md": "hi"}, model.BuildPlanBuildpack, "worker"},
		{"go without dockerfile", map[string]string{"go.mod": "module x"}, model.BuildPlanBuildpack, "go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := NewAppDetectorWithClone(fakeClone(t, tc.files))
			plan, recipe, err := d.DetectBuild(context.Background(), "acme/web", "main")
			if err != nil {
				t.Fatalf("DetectBuild: %v", err)
			}
			if plan.Kind != tc.wantKind {
				t.Errorf("plan kind = %s, want %s", plan.Kind, tc.wantKind)
			}
			if recipe != tc.wantRecipe {
				t.Errorf("recipe = %q, want %q", recipe, tc.wantRecipe)
			}
		})
	}
}

func TestAppDetector_CleansUpWorkdir(t *testing.T) {
	var dir string
	d := NewAppDetectorWithClone(func(_ context.Context, _ github.CloneOptions) (string, error) {
		var err error
		dir, err = os.MkdirTemp("", "cooker-detect-cleanup-*")
		return dir, err
	})
	if _, _, err := d.DetectBuild(context.Background(), "acme/web", ""); err != nil {
		t.Fatalf("DetectBuild: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("workdir %s should have been removed, stat err = %v", dir, err)
	}
}

func TestAppDetector_CloneErrorWrapped(t *testing.T) {
	boom := errors.New("auth required")
	d := NewAppDetectorWithClone(func(_ context.Context, _ github.CloneOptions) (string, error) {
		return "", boom
	})
	_, _, err := d.DetectBuild(context.Background(), "acme/private", "main")
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped clone error, got %v", err)
	}
	if !strings.HasPrefix(err.Error(), "app detect: clone:") {
		t.Errorf("error not package-prefixed: %q", err.Error())
	}
}
