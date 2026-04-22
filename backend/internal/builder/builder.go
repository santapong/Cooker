// Package builder defines the image-builder abstraction used by the
// pipeline executor. Implementations back this with BuildKit (prod),
// a local Docker socket (dev), or a no-op (tests).
package builder

import (
	"context"
	"errors"
	"io"
)

// ErrUnavailable is returned when the underlying build system is not
// reachable or not configured (e.g., BuildKit endpoint unset). Callers
// should treat this as a configuration error, not a build failure.
var ErrUnavailable = errors.New("builder: unavailable")

// Request describes a single image build.
type Request struct {
	// ContextDir is the absolute path to the build context on the host.
	ContextDir string
	// Dockerfile is the path (relative to ContextDir) of the Dockerfile.
	// Empty means "Dockerfile" in the context root.
	Dockerfile string
	// Tags are the fully-qualified image tags to apply
	// (e.g., registry.example.com/app:v1). Must contain at least one entry.
	Tags []string
	// BuildArgs are forwarded as --build-arg.
	BuildArgs map[string]string
	// Platforms enables multi-arch builds via OCI image index.
	// Empty falls back to the builder's default.
	Platforms []string
	// LogWriter receives the build's stdout/stderr stream as it runs.
	// Nil discards the stream.
	LogWriter io.Writer
}

// Result reports the outcome of a successful build.
type Result struct {
	// ImageID is the content-addressable digest of the resulting image
	// (sha256:...). Empty if the backend cannot report one (e.g., in a
	// multi-arch build where the manifest list digest is what matters).
	ImageID string
	// Tags that were actually applied (usually equal to Request.Tags).
	Tags []string
}

// Builder produces container images from a source context.
// Implementations must be safe for concurrent use by multiple goroutines.
type Builder interface {
	Build(ctx context.Context, req Request) (Result, error)
}
