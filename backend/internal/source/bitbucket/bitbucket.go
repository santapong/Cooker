// Package bitbucket parses Bitbucket Server (self-hosted) push events
// and verifies the X-Hub-Signature HMAC-SHA256 header.
//
// Bitbucket Cloud signs nothing today (the operator is expected to
// allowlist Atlassian IPs at the edge). Cooker therefore supports
// Bitbucket Server signatures only — the most common self-hosted
// scenario for teams that don't want their webhooks to traverse the
// public Internet without auth.
package bitbucket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// PushEvent is the subset of the Bitbucket Server "repo:refs_changed"
// payload Cooker consumes.
//
// Bitbucket's schema differs from GitHub's: a single event can carry
// multiple ref changes. Cooker takes the first branch change with a
// non-zero toHash; that's the common case for a push to a single
// branch. Multi-ref pushes are rare and "latest of the batch"
// matches what a CI/CD user would expect.
type PushEvent struct {
	EventKey string `json:"eventKey"` // "repo:refs_changed"
	Changes  []struct {
		Ref struct {
			ID        string `json:"id"`   // "refs/heads/<branch>"
			DisplayID string `json:"displayId"` // "<branch>"
			Type      string `json:"type"` // "BRANCH" | "TAG"
		} `json:"ref"`
		ToHash   string `json:"toHash"`
		FromHash string `json:"fromHash"`
		Type     string `json:"type"` // "ADD" | "UPDATE" | "DELETE"
	} `json:"changes"`
	Repository struct {
		Slug    string `json:"slug"`
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
	} `json:"repository"`
}

// FullName returns "<projectKey>/<slug>" so callers can look the App
// up via the same Apps.GetByRepo seam used by GitHub.
func (e PushEvent) FullName() string {
	if e.Repository.Project.Key == "" || e.Repository.Slug == "" {
		return ""
	}
	return e.Repository.Project.Key + "/" + e.Repository.Slug
}

// Branch returns the first BRANCH change's name, or "" if no branch
// change is present (tag-only pushes etc.).
func (e PushEvent) Branch() string {
	for _, c := range e.Changes {
		if strings.EqualFold(c.Ref.Type, "BRANCH") {
			if c.Ref.DisplayID != "" {
				return c.Ref.DisplayID
			}
			return strings.TrimPrefix(c.Ref.ID, "refs/heads/")
		}
	}
	return ""
}

// IsBranchDelete reports whether the first BRANCH change is a
// deletion. Skip these — nothing to deploy.
func (e PushEvent) IsBranchDelete() bool {
	for _, c := range e.Changes {
		if strings.EqualFold(c.Ref.Type, "BRANCH") {
			return strings.EqualFold(c.Type, "DELETE")
		}
	}
	return false
}

// ErrBadSignature is the sentinel for signature mismatch.
var ErrBadSignature = errors.New("bitbucket: bad signature")

// VerifySignature checks an X-Hub-Signature header (Bitbucket Server
// uses the same "sha256=<hex>" shape as GitHub) against an
// HMAC-SHA256 of the body using the configured secret.
func VerifySignature(secret, body []byte, signature string) error {
	if len(secret) == 0 {
		return errors.New("bitbucket: configured secret is empty")
	}
	if signature == "" {
		return errors.New("bitbucket: X-Hub-Signature header missing")
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return errors.New("bitbucket: expected sha256= prefix")
	}
	gotDigest, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil {
		return errors.New("bitbucket: signature is not hex")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := mac.Sum(nil)
	if !hmac.Equal(gotDigest, want) {
		return ErrBadSignature
	}
	return nil
}
