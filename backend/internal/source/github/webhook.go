// Package github integrates Cooker with GitHub. The webhook
// verifier validates the X-Hub-Signature-256 HMAC that GitHub
// computes with a per-App shared secret, as documented at
// https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries.
package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrBadSignature is returned when the HMAC doesn't match or the
// header is malformed. Always treat this as 401 and drop the event.
var ErrBadSignature = errors.New("github: bad signature")

// VerifySignature performs a constant-time compare of body's HMAC
// (SHA-256 with the given secret) against the sig value from the
// X-Hub-Signature-256 header. sig must carry the "sha256=" prefix
// exactly as GitHub sends it.
func VerifySignature(secret, body []byte, sig string) error {
	const prefix = "sha256="
	if len(secret) == 0 {
		return errors.New("github: empty secret")
	}
	if !strings.HasPrefix(sig, prefix) {
		return ErrBadSignature
	}
	want, err := hex.DecodeString(sig[len(prefix):])
	if err != nil {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	got := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return ErrBadSignature
	}
	return nil
}

// PushEvent is the subset of GitHub's push-event JSON that Cooker
// acts on. Full payload is much larger; we decode only what matters.
type PushEvent struct {
	Ref        string `json:"ref"` // e.g. "refs/heads/main"
	Repository struct {
		FullName string `json:"full_name"` // "owner/name"
	} `json:"repository"`
	After   string `json:"after"`   // new commit SHA
	Before  string `json:"before"`  // old commit SHA
	Deleted bool   `json:"deleted"` // true when the push is a branch / tag delete
}

// zeroSHA is what GitHub sends in `after` when a branch is deleted.
const zeroSHA = "0000000000000000000000000000000000000000"

// IsBranchDelete reports whether the push was a branch / tag
// deletion. Cooker should ignore these — there's nothing to deploy.
func (p *PushEvent) IsBranchDelete() bool {
	return p.Deleted || p.After == zeroSHA
}

// Branch extracts the branch name from the push-event Ref.
func (p *PushEvent) Branch() string {
	const prefix = "refs/heads/"
	if strings.HasPrefix(p.Ref, prefix) {
		return p.Ref[len(prefix):]
	}
	return ""
}
