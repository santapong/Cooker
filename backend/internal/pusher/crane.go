package pusher

import (
	"context"
	"fmt"
)

// Crane pushes images using go-containerregistry. A production
// deployment points this at the bundled CNCF Distribution registry
// or a user-provided registry.
//
// The actual go-containerregistry wiring is intentionally deferred
// (roadmap Phase 1.3): pulling it in requires a network-reachable
// registry for meaningful tests. This type is defined now so
// downstream code can program against the production Pusher.
type Crane struct {
	// UserAgent, when non-empty, overrides the HTTP User-Agent.
	UserAgent string
}

func NewCrane() *Crane { return &Crane{UserAgent: "cooker/0.1"} }

// Push is not yet wired; returns ErrUnavailable so the executor can
// surface a descriptive error.
func (c *Crane) Push(_ context.Context, _ Request) (Result, error) {
	return Result{}, fmt.Errorf("%w: go-containerregistry client not yet wired (roadmap Phase 1.3)", ErrUnavailable)
}

var _ Pusher = (*Crane)(nil)
