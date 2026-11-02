package pusher

import (
	"context"
	"fmt"
)

// Noop is a Pusher that performs no work. It returns a fixed fake
// digest so downstream stages have a non-empty reference.
type Noop struct{}

func (Noop) Push(_ context.Context, req Request) (Result, error) {
	if req.Artifact != nil {
		return Result{}, fmt.Errorf("%w: noop pusher cannot publish a built OCI artifact", ErrUnavailable)
	}
	// Even the no-op writes the canonical success line so unit-test
	// pipelines exercise the LogWriter wiring end-to-end. Target may be
	// empty in malformed tests; emit something useful regardless.
	target := req.Target
	if target == "" {
		target = "<unset>"
	}
	logf(req.LogWriter, "Pushed image to %s (noop)\n", target)
	return Result{Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"}, nil
}

var _ Pusher = Noop{}
