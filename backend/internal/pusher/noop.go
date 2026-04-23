package pusher

import "context"

// Noop is a Pusher that performs no work. It returns a fixed fake
// digest so downstream stages have a non-empty reference.
type Noop struct{}

func (Noop) Push(_ context.Context, _ Request) (Result, error) {
	return Result{Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"}, nil
}

var _ Pusher = Noop{}
