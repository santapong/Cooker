package model

import "time"

// License is the installed self-hosted license for this Cooker instance
// (M2 self-hosted licensing — see docs/launch/01-billing-monetization.md
// §4). Cooker's paid tiers are a feature-flag-and-replica license, not a
// usage meter: a signed license string encodes the plan, seat/replica
// allowance, and the set of feature flags it unlocks, and an operator
// installs exactly one at a time.
//
// This model is pure persistence. The signed RawToken is verified by the
// licensing service (M2-T2), not here; the store keeps only the installed
// record and the decoded claims so the admin UI can render the current
// entitlement without re-verifying on every read. There is at most one
// active license per instance — Set replaces it, Delete clears it.
type License struct {
	// ID is the license's stable identifier. For the single active
	// license the store uses a fixed sentinel id (see store impls), but
	// the field is kept so a future "license history" table can key on it.
	ID string `json:"id" db:"id"`
	// RawToken is the signed license string as issued (the verifiable
	// artefact an operator pastes in). Never serialised to clients
	// (json:"-"): it is opaque credential material and a read endpoint
	// must not echo it. Signature verification lives in the service layer.
	RawToken string `json:"-" db:"raw_token"`
	// Plan is the tier this license grants, e.g. "free" (explorer),
	// "crew", or "constellation". Free-form so new tiers don't require a
	// migration.
	Plan string `json:"plan" db:"plan"`
	// Seats is the licensed allowance. Per the mockup's per-replica
	// model this is the replica count; 0 means unlimited / unset.
	Seats int `json:"seats" db:"seats"`
	// Features is the set of feature flags this license unlocks (e.g.
	// "multi-replica", "oidc", "secrets-vault"). Persisted as a TEXT[].
	Features []string `json:"features" db:"features"`
	// Customer is the licensee name embedded in the signed token, shown
	// in the admin UI ("Licensed to: …").
	Customer string `json:"customer" db:"customer"`
	// IssuedAt is when the license was signed/issued by Cooker.
	IssuedAt time.Time `json:"issuedAt" db:"issued_at"`
	// ExpiresAt is when the license stops being active. nil = perpetual
	// (e.g. the free tier never expires).
	ExpiresAt *time.Time `json:"expiresAt,omitempty" db:"expires_at"`
	// InstalledAt is when an operator installed this license on this
	// instance.
	InstalledAt time.Time `json:"installedAt" db:"installed_at"`
	// InstalledBySub / InstalledByEmail identify the human who installed
	// the license, for the audit trail / admin UI.
	InstalledBySub   string `json:"installedBySub" db:"installed_by_sub"`
	InstalledByEmail string `json:"installedByEmail" db:"installed_by_email"`
}

// IsExpired reports whether the license's expiry has passed as of now. A
// nil ExpiresAt is perpetual and never expires. Mirrors APIToken.Expired.
func (l *License) IsExpired(now time.Time) bool {
	return l.ExpiresAt != nil && !l.ExpiresAt.After(now)
}

// IsActive reports whether the license currently grants its entitlement:
// it must not be expired as of now. This is a pure helper over the
// decoded claims — it does NOT verify the RawToken signature, which is
// the licensing service's responsibility (M2-T2). A nil receiver is
// treated as "no license installed" and is never active.
func (l *License) IsActive(now time.Time) bool {
	if l == nil {
		return false
	}
	return !l.IsExpired(now)
}
