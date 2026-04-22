// Package buildplan inspects a source directory and decides how to
// build it. Precedence (matching the roadmap): Dockerfile →
// docker-compose → Paketo buildpacks.
package buildplan

import (
	"os"
	"path/filepath"

	"github.com/cooker-ci/cooker/internal/model"
)

// FS is the subset of the filesystem the detector needs. The
// default implementation (osFS) uses the real OS; tests pass a
// mapFS for deterministic behaviour.
type FS interface {
	Stat(path string) (os.FileInfo, error)
}

type osFS struct{}

func (osFS) Stat(p string) (os.FileInfo, error) { return os.Stat(p) }

// Detect returns the best build plan for the contents of dir. Falls
// back to a Paketo buildpack build when no Dockerfile or compose
// file is present, so untouched repos still build.
func Detect(dir string) *model.BuildPlan {
	return detect(dir, osFS{})
}

func detect(dir string, fs FS) *model.BuildPlan {
	// 1. Dockerfile at the repo root wins.
	if fileExists(fs, filepath.Join(dir, "Dockerfile")) {
		return &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile"}
	}
	// 2. docker-compose.yml / compose.yaml is next.
	for _, name := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
		if fileExists(fs, filepath.Join(dir, name)) {
			return &model.BuildPlan{Kind: model.BuildPlanCompose, Path: name}
		}
	}
	// 3. Fall back to buildpacks. The builder resolves the concrete
	// buildpack set from the language files it sees in the context.
	return &model.BuildPlan{Kind: model.BuildPlanBuildpack}
}

func fileExists(fs FS, path string) bool {
	info, err := fs.Stat(path)
	return err == nil && !info.IsDir()
}
