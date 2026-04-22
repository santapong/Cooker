package builder

import (
	"context"
	"fmt"
)

// BuildKit builds images via BuildKit's gRPC frontend. A production
// deployment runs buildkitd as a Deployment inside the target cluster
// and points Cooker at its Service endpoint.
//
// The gRPC wiring (github.com/moby/buildkit/client) is intentionally
// not imported here yet: pulling it in requires solvers, frontends,
// and network access to the buildkitd instance for any meaningful
// test. This type is defined so the rest of the codebase can already
// program against the final Builder implementation; Build returns
// ErrUnavailable until the wiring lands.
type BuildKit struct {
	// Addr is the buildkitd endpoint (e.g., tcp://buildkit:1234 or
	// kube-pod://namespace/pod). Empty is treated as unconfigured.
	Addr string
	// OCIRegistryAuth, when non-nil, returns {username, password} for
	// pushing built images to the given registry host.
	OCIRegistryAuth func(registry string) (string, string, bool)
}

// NewBuildKit returns a BuildKit builder for the given endpoint.
func NewBuildKit(addr string) *BuildKit { return &BuildKit{Addr: addr} }

// Build is not yet implemented; it returns ErrUnavailable with a
// descriptive message so the executor can surface a useful error to
// the operator. See the roadmap: Phase 1.3.
func (b *BuildKit) Build(_ context.Context, _ Request) (Result, error) {
	if b == nil || b.Addr == "" {
		return Result{}, fmt.Errorf("%w: BuildKit endpoint not configured", ErrUnavailable)
	}
	return Result{}, fmt.Errorf("%w: BuildKit gRPC client not yet wired (see roadmap Phase 1.3)", ErrUnavailable)
}

var _ Builder = (*BuildKit)(nil)
