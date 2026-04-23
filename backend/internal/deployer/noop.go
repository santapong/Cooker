package deployer

import "context"

// Noop is a Deployer that performs no work. Used as the default so
// Execute is safe in tests and dry runs.
type Noop struct{}

func (Noop) Deploy(_ context.Context, _ Request) (Result, error) {
	return Result{}, nil
}

var _ Deployer = Noop{}
