package builder

import (
	"context"
	"fmt"
	"io"
)

// Noop is a Builder that performs no work and reports success. It is
// used when Cooker is configured without a real build backend (for
// development, smoke tests, or the dry-run mode of the executor).
type Noop struct{}

// Build logs the request to the provided writer, if any, and returns
// a Result that echoes the request tags with a fake digest so
// downstream stages have a non-empty image reference to forward.
func (Noop) Build(_ context.Context, req Request) (Result, error) {
	if req.LogWriter != nil {
		fmt.Fprintf(req.LogWriter, "noop: would build %s (dockerfile=%q, tags=%v)\n",
			req.ContextDir, req.Dockerfile, req.Tags)
		// Ensure the writer flushes when it's a bufio or pipe.
		if f, ok := req.LogWriter.(interface{ Sync() error }); ok {
			_ = f.Sync()
		}
	}
	return Result{ImageID: "sha256:0000000000000000000000000000000000000000000000000000000000000000", Tags: append([]string(nil), req.Tags...)}, nil
}

// Compile-time checks that Noop satisfies Builder and that io.Discard
// continues to satisfy io.Writer (defensive — failing the import would
// be a confusing compile error in this package otherwise).
var _ Builder = Noop{}
var _ io.Writer = io.Discard
