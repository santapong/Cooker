package buildplan

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cooker-ci/cooker/internal/model"
)

type fakeFileInfo struct {
	name string
	dir  bool
}

func (f fakeFileInfo) Name() string     { return f.name }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool      { return f.dir }
func (fakeFileInfo) Sys() interface{}   { return nil }

type mapFS map[string]bool

func (m mapFS) Stat(p string) (os.FileInfo, error) {
	if _, ok := m[p]; ok {
		return fakeFileInfo{name: filepath.Base(p)}, nil
	}
	return nil, &fs.PathError{Err: errors.New("not found")}
}

func TestDetect_Dockerfile(t *testing.T) {
	plan := detect("/repo", mapFS{"/repo/Dockerfile": true})
	if plan.Kind != model.BuildPlanDockerfile || plan.Path != "Dockerfile" {
		t.Errorf("got %+v", plan)
	}
}

func TestDetect_ComposePreferredOverBuildpack(t *testing.T) {
	plan := detect("/repo", mapFS{"/repo/docker-compose.yml": true})
	if plan.Kind != model.BuildPlanCompose {
		t.Errorf("got %+v", plan)
	}
	if plan.Path != "docker-compose.yml" {
		t.Errorf("path: %q", plan.Path)
	}
}

func TestDetect_DockerfileBeatsCompose(t *testing.T) {
	plan := detect("/repo", mapFS{
		"/repo/Dockerfile":         true,
		"/repo/docker-compose.yml": true,
	})
	if plan.Kind != model.BuildPlanDockerfile {
		t.Errorf("Dockerfile should win; got %+v", plan)
	}
}

func TestDetect_FallbackBuildpack(t *testing.T) {
	plan := detect("/repo", mapFS{})
	if plan.Kind != model.BuildPlanBuildpack {
		t.Errorf("got %+v", plan)
	}
}

func TestDetect_RecognisesAlternateComposeNames(t *testing.T) {
	for _, name := range []string{"docker-compose.yaml", "compose.yml", "compose.yaml"} {
		plan := detect("/repo", mapFS{"/repo/" + name: true})
		if plan.Kind != model.BuildPlanCompose {
			t.Errorf("%s: got %+v", name, plan)
		}
	}
}
