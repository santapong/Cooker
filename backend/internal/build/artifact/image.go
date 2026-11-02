// Package artifact owns private, transient image handoffs between build and push.
// Artifact paths are never accepted from a pipeline or serialized into a run.
package artifact

import (
	"fmt"
	"os"
	"sync"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
)

// Image is an opaque capability for one locally exported OCI image or index.
// Its owner must Close after all consumers finish, including failed runs.
type Image struct {
	dir  string
	once sync.Once
	err  error
}

func NewImage() (*Image, error) {
	dir, err := os.MkdirTemp("", "cooker-build-")
	if err != nil {
		return nil, err
	}
	return &Image{dir: dir}, nil
}

// OutputDir is only for the trusted builder's OCI exporter. There is no
// constructor accepting an arbitrary path and no exported path field.
func (a *Image) OutputDir() string { return a.dir }

// Contents returns the single exported descriptor and its local content store.
// A build may produce a multi-platform index, but the handoff itself has one root.
func (a *Image) Contents() (layout.Path, v1.Descriptor, error) {
	if a == nil || a.dir == "" {
		return "", v1.Descriptor{}, fmt.Errorf("image artifact unavailable")
	}
	p, err := layout.FromPath(a.dir)
	if err != nil {
		return "", v1.Descriptor{}, fmt.Errorf("read image artifact: %w", err)
	}
	idx, err := p.ImageIndex()
	if err != nil {
		return "", v1.Descriptor{}, err
	}
	manifest, err := idx.IndexManifest()
	if err != nil {
		return "", v1.Descriptor{}, err
	}
	if len(manifest.Manifests) != 1 {
		return "", v1.Descriptor{}, fmt.Errorf("image artifact must contain exactly one root, found %d", len(manifest.Manifests))
	}
	return p, manifest.Manifests[0], nil
}

func (a *Image) Close() error {
	if a == nil {
		return nil
	}
	a.once.Do(func() { a.err = os.RemoveAll(a.dir) })
	return a.err
}
