package gitops

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
)

// Noop is a Writer that pretends to commit and returns a hash of
// the file contents as a fake SHA. Used as the default so Execute
// is safe without a real Git backend.
type Noop struct{}

func (Noop) Commit(_ context.Context, req Request) (Result, error) {
	content := req.Content
	if req.Image != "" {
		content = bytes.ReplaceAll(content, []byte("${IMAGE}"), []byte(req.Image))
	}
	h := sha1.Sum(content)
	return Result{CommitSHA: hex.EncodeToString(h[:])}, nil
}

var _ Writer = Noop{}
