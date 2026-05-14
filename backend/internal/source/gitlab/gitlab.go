// Package gitlab parses GitLab push-event webhook payloads and
// verifies the X-Gitlab-Token header. GitLab's webhook signing model
// differs from GitHub's HMAC-SHA256 — the operator configures a
// literal "Secret Token" on the GitLab side, and GitLab sends that
// token verbatim in the X-Gitlab-Token header on each request. We
// constant-time-compare against the App's stored secret.
package gitlab

import (
	"crypto/subtle"
	"errors"
	"strings"
)

// PushEvent is the subset of the GitLab "Push Hook" payload Cooker
// consumes. Schema: https://docs.gitlab.com/user/project/integrations/webhook_events/#push-events
//
// Field names follow GitLab's JSON wire shape, not the Go convention,
// because we unmarshal directly.
type PushEvent struct {
	ObjectKind string `json:"object_kind"` // "push"
	Ref        string `json:"ref"`         // "refs/heads/<branch>" or "refs/tags/<tag>"
	Before     string `json:"before"`      // "000...000" on branch create
	After      string `json:"after"`       // "000...000" on branch delete
	Project    struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	Repository struct {
		// Mirror of Project.PathWithNamespace for GitHub-style code
		// paths that look up by Repository.FullName.
		Name string `json:"name"`
	} `json:"repository"`
}

// FullName returns the canonical owner/name (or group/subgroup/name)
// identifier used by the App store's GetByRepo. Mirrors GitHub's
// Repository.FullName so callers can treat the value uniformly.
func (e PushEvent) FullName() string { return e.Project.PathWithNamespace }

// Branch returns the short branch name (no "refs/heads/" prefix), or
// "" when the ref is not a branch (tag pushes etc.).
func (e PushEvent) Branch() string {
	const prefix = "refs/heads/"
	if !strings.HasPrefix(e.Ref, prefix) {
		return ""
	}
	return strings.TrimPrefix(e.Ref, prefix)
}

// IsBranchDelete reports whether the event represents a branch
// deletion (after == 40 zeros). Cooker ignores these because there's
// nothing to deploy.
func (e PushEvent) IsBranchDelete() bool {
	return e.After == "0000000000000000000000000000000000000000"
}

// ErrTokenMismatch is returned by VerifyToken when the provided
// token does not match the App's configured secret.
var ErrTokenMismatch = errors.New("gitlab: token mismatch")

// VerifyToken compares the secret configured on the App against the
// token GitLab sent in X-Gitlab-Token. Uses subtle.ConstantTimeCompare
// to avoid timing-side-channel leakage. Returns ErrTokenMismatch on
// mismatch; nil on success; an error string when either argument is
// empty (defensive: a misconfigured webhook should fail loud, not
// silently allow).
func VerifyToken(secret []byte, provided string) error {
	if len(secret) == 0 {
		return errors.New("gitlab: configured secret is empty")
	}
	if provided == "" {
		return errors.New("gitlab: X-Gitlab-Token header missing")
	}
	if subtle.ConstantTimeCompare(secret, []byte(provided)) == 1 {
		return nil
	}
	return ErrTokenMismatch
}
