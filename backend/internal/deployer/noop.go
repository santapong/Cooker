package deployer

import "context"

// Noop is a Deployer that performs no work. Used as the default so
// Execute is safe in tests and dry runs.
type Noop struct{}

func (Noop) Deploy(_ context.Context, req Request) (Result, error) {
	// Surface a single canonical line so unit-test pipelines exercise
	// the LogWriter wiring end-to-end; the line uses Kind so deploy
	// runs of either KindManifest, KindHelm, or KindCompose all leave
	// a recognisable trace.
	kind := string(req.Kind)
	if kind == "" {
		kind = "noop"
	}
	logf(req.LogWriter, "Applied %s (noop)\n", kind)
	return Result{}, nil
}

var _ Deployer = Noop{}
