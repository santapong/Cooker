// Package pusher defines the OCI image-push abstraction used by the
// pipeline executor. Implementations back this with
// go-containerregistry (prod), a local Docker socket (dev), or a
// no-op (tests).
package pusher

import (
	"context"
	"errors"
)

// ErrUnavailable is returned when the push backend is not reachable or
// not configured (e.g., no credentials, no network).
var ErrUnavailable = errors.New("pusher: unavailable")

// Request identifies one image to push. Source resolves to an image
// already present on the host (by tag or digest); Target is the
// fully-qualified destination reference.
type Request struct {
	Source string
	Target string
	// Auth, when non-nil, supplies {username, password} for the target
	// registry host. Nil falls back to the local docker config /
	// credential helpers.
	Auth func(host string) (user, pass string, ok bool)
}

// Result reports the outcome of a successful push.
type Result struct {
	// Digest is the content-addressable digest returned by the registry
	// after the push (sha256:...).
	Digest string
}

// Pusher copies a local image to a remote OCI registry.
// Implementations must be safe for concurrent use.
type Pusher interface {
	Push(ctx context.Context, req Request) (Result, error)
}
