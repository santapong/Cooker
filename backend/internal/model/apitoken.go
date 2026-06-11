package model

import "time"

// APIToken is a long-lived bearer credential a script or external CI
// system uses to authenticate against the Cooker API in place of the
// browser OIDC flow (product-plan Tier 1). The plaintext credential is
// `ck_<base64url>`; only its SHA-256 Hash is persisted. The plaintext is
// returned exactly once, at creation, and is unrecoverable afterwards —
// revocation is a row delete.
//
// A token authenticates as exactly one Role, snapshotted at creation and
// capped to <= the creating user's own role. Token-authenticated requests
// cannot themselves create or delete tokens (no self-replication) — see
// service.APITokenService and SECURITY.md.
type APIToken struct {
	ID string `json:"id" db:"id"`
	// Name is an operator-supplied label ("ci-deploy", "laptop") shown in
	// listings. Not unique.
	Name string `json:"name" db:"name"`
	// DisplayPrefix is the first 12 characters of the plaintext token
	// (e.g. "ck_AbCd1234"), kept so a token is identifiable in a list
	// without revealing the secret. Not sufficient to authenticate.
	DisplayPrefix string `json:"displayPrefix" db:"display_prefix"`
	// Hash is the hex-encoded SHA-256 of the full plaintext token. Never
	// serialised to clients (json:"-"): it is the only stored secret
	// material and a read endpoint must not echo it.
	Hash string `json:"-" db:"hash"`
	// Role is the single RBAC role this token authenticates as.
	Role string `json:"role" db:"role"`
	// CreatedBySub / CreatedByEmail identify the human who minted the
	// token. Ownership for list/delete is keyed on CreatedBySub.
	CreatedBySub   string `json:"createdBySub" db:"created_by_sub"`
	CreatedByEmail string `json:"createdByEmail" db:"created_by_email"`

	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	// ExpiresAt is when the token stops authenticating. nil = never
	// expires (operators are advised to set a short expiry for CI tokens).
	ExpiresAt *time.Time `json:"expiresAt,omitempty" db:"expires_at"`
	// LastUsedAt is the most recent authenticated use, throttled to at
	// most one write per minute per token. nil = never used.
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty" db:"last_used_at"`
}

// Expired reports whether the token's expiry has passed as of now. A nil
// ExpiresAt never expires.
func (t *APIToken) Expired(now time.Time) bool {
	return t.ExpiresAt != nil && !t.ExpiresAt.After(now)
}
