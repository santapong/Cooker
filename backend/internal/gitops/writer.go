// Package gitops writes manifests/Helm values into a Git repo so a
// controller like ArgoCD or Flux reconciles them into the cluster.
// Cooker never pushes to the cluster directly for GitOps flows —
// it just hands the change to Git and the controller takes over.
package gitops

import (
	"context"
	"errors"
)

// ErrUnavailable is returned when no Git backend is configured
// (e.g., this build didn't bring in go-git).
var ErrUnavailable = errors.New("gitops: unavailable")

// Request describes one commit.
type Request struct {
	Repo    string // e.g., "git@github.com:org/gitops.git"
	Branch  string // default "main"
	Path    string // path within the repo to write
	Content []byte // file contents
	Message string // commit message
	Author  string // "name <email>" (falls back to "Cooker <bot@cooker>")
	// Image, when non-empty, is substituted for "${IMAGE}" in Content
	// before committing.
	Image string
}

// Result reports the outcome of a successful commit.
type Result struct {
	CommitSHA string
	URL       string // web URL of the commit if the backend knows one
}

// Writer commits files to a remote Git repository. Implementations
// must be safe for concurrent use (or document otherwise).
type Writer interface {
	Commit(ctx context.Context, req Request) (Result, error)
}
