package gitops

import (
	"context"
	"fmt"
)

// GoGit commits files using go-git (github.com/go-git/go-git).
// Intentionally not yet wired — go-git is a heavyweight import and
// any real test needs a reachable remote. Defined so downstream
// code can program against a production Writer.
type GoGit struct {
	// SSHKeyPath is the path to a private key authorised on the
	// target repositories. When empty, the writer falls back to the
	// ambient ssh-agent or HTTPS credentials.
	SSHKeyPath string
}

func NewGoGit() *GoGit { return &GoGit{} }

func (*GoGit) Commit(_ context.Context, _ Request) (Result, error) {
	return Result{}, fmt.Errorf("%w: go-git client not yet wired (roadmap Phase 6)", ErrUnavailable)
}

var _ Writer = (*GoGit)(nil)
