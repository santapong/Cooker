// Package license implements offline verification of Cooker self-hosted
// license tokens (M2 self-hosted licensing — see
// docs/launch/01-billing-monetization.md §4). It is a pure, dependency-
// free verifier: given a license token and the vendor's Ed25519 public
// key it checks the signature and decodes the claims. It does NOT decide
// whether a license is still valid (expiry) — that is a policy decision
// the caller (the licensing service / entitlements engine) makes, because
// an expired license still degrades to Free rather than erroring.
//
// # Token format
//
// A license token is two base64url (no padding) segments joined by a dot:
//
//	base64url(payloadJSON) + "." + base64url(ed25519signature)
//
// where:
//   - payloadJSON is the canonical JSON encoding of Claims (UTF-8 bytes).
//   - ed25519signature is the 64-byte Ed25519 signature computed over the
//     RAW payloadJSON bytes (the bytes that base64url-decode out of the
//     first segment), using the vendor's private signing key.
//
// Verification re-derives the signed message from the first segment's
// decoded bytes, so a tamper of either the payload or the signature fails
// the Ed25519 check. The vendor holds the private key out of band (never
// in the repo); only the public key is distributed to operators, who set
// it via COOKER_LICENSE_PUBLIC_KEY. The matching signing CLI is
// cmd/cooker-license.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Claims is the decoded, verified payload of a license token. All time
// fields are Unix seconds in the wire format; the helpers expose them as
// time.Time. A zero Exp means a perpetual license (never expires).
type Claims struct {
	// Plan is the tier this license grants: "free", "crew",
	// "constellation". Free-form so a new tier needs no code change here.
	Plan string `json:"plan"`
	// Seats is the licensed allowance (per-replica count in the mockup's
	// model). 0 means unlimited / unset.
	Seats int `json:"seats"`
	// Features is the explicit set of feature flags the license unlocks.
	// The entitlements engine unions these with the plan's baseline set.
	Features []string `json:"features,omitempty"`
	// Customer is the licensee name embedded by the vendor at signing
	// time, surfaced in the admin UI ("Licensed to: …").
	Customer string `json:"customer"`
	// IssuedAt is when the vendor signed the token (Unix seconds).
	IssuedAt int64 `json:"iat"`
	// ExpiresAt is when the license stops being active (Unix seconds).
	// 0 = perpetual (never expires).
	ExpiresAt int64 `json:"exp,omitempty"`
}

// IssuedAtTime returns the issued-at claim as a time.Time (UTC). A zero
// IssuedAt yields the zero time.
func (c *Claims) IssuedAtTime() time.Time {
	if c.IssuedAt == 0 {
		return time.Time{}
	}
	return time.Unix(c.IssuedAt, 0).UTC()
}

// ExpiresAtTime returns the expiry claim as a *time.Time (UTC), or nil
// for a perpetual license (Exp == 0). The nil-vs-value shape matches
// model.License.ExpiresAt so the service can copy it across directly.
func (c *Claims) ExpiresAtTime() *time.Time {
	if c.ExpiresAt == 0 {
		return nil
	}
	t := time.Unix(c.ExpiresAt, 0).UTC()
	return &t
}

// IsExpired reports whether the license has expired as of now. A
// perpetual license (Exp == 0) is never expired. This is a convenience
// for callers; Verify deliberately does not call it.
func (c *Claims) IsExpired(now time.Time) bool {
	exp := c.ExpiresAtTime()
	return exp != nil && !exp.After(now)
}

// Errors returned by Verify. Callers map these to HTTP/log behaviour at
// their boundary (the service translates them to a typed install error).
var (
	// ErrMalformed is returned when the token is not the expected
	// two-segment base64url.payload.base64url.sig shape, or a segment is
	// not valid base64url, or the payload is not valid JSON, or the
	// signature is not 64 bytes.
	ErrMalformed = errors.New("license: malformed token")
	// ErrBadSignature is returned when the Ed25519 signature does not
	// verify against the supplied public key (tampered payload, tampered
	// signature, or wrong key).
	ErrBadSignature = errors.New("license: signature verification failed")
	// ErrBadKey is returned when the supplied public key is not a valid
	// Ed25519 public key (wrong length).
	ErrBadKey = errors.New("license: invalid public key")
)

// b64 is the URL-safe base64 alphabet WITHOUT padding — chosen so a token
// is a single clean line an operator can paste into an env var or the
// admin UI without worrying about '=' or '+/' characters.
var b64 = base64.RawURLEncoding

// Encode builds a token from a payload and signature, applying the
// canonical base64url(payload).base64url(sig) framing. It is exported so
// the signing CLI (cmd/cooker-license) produces tokens in exactly the
// shape Verify expects, with no second implementation to drift.
func Encode(payloadJSON, sig []byte) string {
	return b64.EncodeToString(payloadJSON) + "." + b64.EncodeToString(sig)
}

// MarshalClaims produces the canonical payload bytes for a set of claims.
// Both the signer and any re-encode path go through this so the bytes
// that get signed are deterministic. Verify never re-marshals (it signs
// over the decoded first segment verbatim), but the signer uses this.
func MarshalClaims(c *Claims) ([]byte, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("license: marshal claims: %w", err)
	}
	return b, nil
}

// Verify checks a license token's Ed25519 signature against pubKey and
// returns the decoded claims. It does NOT check expiry — the caller
// decides what an expired license means (Cooker degrades to Free rather
// than failing). A malformed token, a bad signature, or a wrong/short
// public key returns the corresponding sentinel error and nil claims.
func Verify(token string, pubKey ed25519.PublicKey) (*Claims, error) {
	if len(pubKey) != ed25519.PublicKeySize {
		return nil, ErrBadKey
	}
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrMalformed
	}
	payloadJSON, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: payload base64: %v", ErrMalformed, err)
	}
	sig, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: signature base64: %v", ErrMalformed, err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: signature is %d bytes, want %d", ErrMalformed, len(sig), ed25519.SignatureSize)
	}
	// The signature is over the raw decoded payload bytes — exactly what
	// the signer signed. We verify BEFORE unmarshalling so we never act on
	// untrusted JSON structure.
	if !ed25519.Verify(pubKey, payloadJSON, sig) {
		return nil, ErrBadSignature
	}
	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("%w: payload json: %v", ErrMalformed, err)
	}
	return &claims, nil
}

// DecodePublicKey parses a base64-encoded Ed25519 public key (the value
// an operator sets in COOKER_LICENSE_PUBLIC_KEY). It accepts both the
// URL-safe (no padding) and the standard base64 alphabets so an operator
// can paste whichever the signing CLI emitted. Returns ErrBadKey on any
// decode failure or wrong length.
func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ErrBadKey
	}
	var raw []byte
	var err error
	if raw, err = base64.StdEncoding.DecodeString(s); err != nil {
		if raw, err = b64.DecodeString(s); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBadKey, err)
		}
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: decoded to %d bytes, want %d", ErrBadKey, len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}
