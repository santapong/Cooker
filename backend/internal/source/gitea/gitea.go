// Package gitea parses Gitea push-event webhook payloads and verifies
// the X-Gitea-Signature HMAC-SHA256 header.
//
// Gitea's payload shape is intentionally GitHub-compatible (the team
// even ships GitHub-style payloads as a switchable webhook content
// type), so this package mirrors the GitHub source package closely
// — only the signature-header name differs (Gitea ships the raw hex
// digest without a "sha256=" prefix).
package gitea

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// PushEvent matches Gitea's push-event payload. See
// https://docs.gitea.com/usage/webhooks for the schema.
type PushEvent struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"` // Gitea sets this on branch-delete
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// FullName mirrors GitHub's Repository.FullName for the App lookup.
func (e PushEvent) FullName() string { return e.Repository.FullName }

// Branch returns the short branch name, or "" for non-branch refs.
func (e PushEvent) Branch() string {
	const prefix = "refs/heads/"
	if !strings.HasPrefix(e.Ref, prefix) {
		return ""
	}
	return strings.TrimPrefix(e.Ref, prefix)
}

// IsBranchDelete uses Gitea's explicit `deleted` flag (preferred) and
// falls back to the GitHub-style all-zeros `after` for older Gitea
// versions that don't emit `deleted`.
func (e PushEvent) IsBranchDelete() bool {
	return e.Deleted || e.After == "0000000000000000000000000000000000000000"
}

// ErrBadSignature is the sentinel for signature mismatch.
var ErrBadSignature = errors.New("gitea: bad signature")

// VerifySignature checks the X-Gitea-Signature header (raw hex digest,
// no "sha256=" prefix — distinct from GitHub / Bitbucket) against an
// HMAC-SHA256 of the body using the configured secret.
func VerifySignature(secret, body []byte, signature string) error {
	if len(secret) == 0 {
		return errors.New("gitea: configured secret is empty")
	}
	if signature == "" {
		return errors.New("gitea: X-Gitea-Signature header missing")
	}
	gotDigest, err := hex.DecodeString(signature)
	if err != nil {
		return errors.New("gitea: signature is not hex")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := mac.Sum(nil)
	if !hmac.Equal(gotDigest, want) {
		return ErrBadSignature
	}
	return nil
}
