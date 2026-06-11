package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// APITokenPrefix is the marker every plaintext API token carries. The
// auth middleware tests for it to route a bearer credential to the
// token-auth path (SHA-256 + hash-index lookup) cheaply, before paying
// for an OIDC or local-JWT verify. It is intentionally short and
// unmistakable, in the style of GitHub's `ghp_` / Stripe's `sk_`.
const APITokenPrefix = "ck_"

// apiTokenRandomBytes is the number of crypto/rand bytes behind the
// random portion of a token. 32 bytes = 256 bits, well past brute-force
// reach and matching the WS-ticket entropy budget.
const apiTokenRandomBytes = 32

// apiTokenDisplayLen is how many leading plaintext characters are kept as
// the human-readable DisplayPrefix (e.g. "ck_AbCd1234"). Enough to tell
// two tokens apart in a list, far too few to authenticate with.
const apiTokenDisplayLen = 12

// GenerateAPIToken mints a fresh plaintext token. It returns the
// plaintext (shown to the operator exactly once), its hex SHA-256 hash
// (the only thing persisted), and the display prefix (first
// apiTokenDisplayLen chars). The plaintext is `ck_` + base64url(32 random
// bytes); base64url is unpadded so the credential is a single clean
// word with no `=` to trip up shells or URLs.
func GenerateAPIToken() (plaintext, hash, displayPrefix string, err error) {
	buf := make([]byte, apiTokenRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("apitoken: read random: %w", err)
	}
	plaintext = APITokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	hash = HashAPIToken(plaintext)
	displayPrefix = plaintext
	if len(plaintext) > apiTokenDisplayLen {
		displayPrefix = plaintext[:apiTokenDisplayLen]
	}
	return plaintext, hash, displayPrefix, nil
}

// HashAPIToken returns the lowercase hex SHA-256 of a plaintext token.
// Used both at creation (to store the hash) and at verify time (to look
// the token up by hash). Deterministic and allocation-light so it is
// safe to call on the auth hot path.
func HashAPIToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// LooksLikeAPIToken reports whether a bearer credential is shaped like an
// API token (carries the `ck_` prefix). The auth middleware uses this to
// decide whether to take the token-auth branch; it is a cheap routing
// test, NOT an authentication check — a matching prefix still has to hash
// to a stored, unexpired token.
func LooksLikeAPIToken(credential string) bool {
	return strings.HasPrefix(credential, APITokenPrefix)
}
