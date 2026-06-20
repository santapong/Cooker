// Package entitlements resolves a Cooker install's plan into the concrete
// set of features it may use (M2 self-hosted licensing — see
// docs/launch/01-billing-monetization.md §1.4 and §4). It is pure and
// dependency-free: given a model.License (or nil) it returns an
// Entitlements value, never an error. The defining rule is
// degrade-to-Free: a missing, nil, or expired license yields the Free
// (Explorer) baseline rather than failing — an expired license must never
// brick a self-hosted instance, only lock the paid features above Free.
//
// The same Entitlements shape is intended to serve both the self-hosted
// license path (today) and a future hosted/Stripe path (B1), so the rest
// of the system never learns where a plan came from.
package entitlements

import (
	"time"

	"github.com/santapong/cooker/internal/model"
)

// Plan identifiers. Free-form strings elsewhere in the system map onto
// these; an unknown plan degrades to Free.
const (
	PlanFree          = "free"
	PlanCrew          = "crew"
	PlanConstellation = "constellation"
)

// Feature flag keys. These are the gateable commercial capabilities; the
// core CI/CD loop (pipelines, runs, builds, deploys) is deliberately NOT
// represented here and is never gated (mockup promises "unlimited"). Keys
// are stable strings so the license payload and the entitlement gate can
// reference them without importing this package's constants.
const (
	// FeatureMultiReplica unlocks HA / multi-replica operation (the
	// mockup's per-replica axis). Soft-gated in practice (warn, not
	// crash) but represented here so the UI can surface it.
	FeatureMultiReplica = "multi_replica"
	// FeatureOIDCMFA unlocks OIDC + RBAC + MFA.
	FeatureOIDCMFA = "oidc_mfa"
	// FeatureManagedSecrets unlocks the external secrets backends
	// (Vault / AWS / GCP / KeepSave) beyond the built-in Postgres AES-GCM.
	FeatureManagedSecrets = "managed_secrets"
	// FeatureSSOGroupMap unlocks SSO group→role mapping (Constellation).
	FeatureSSOGroupMap = "sso_group_map"
	// FeatureAuditOTLP unlocks the full audit log + OTLP export.
	FeatureAuditOTLP = "audit_otlp"
	// FeatureAirGapped marks the air-gapped Constellation capability.
	FeatureAirGapped = "air_gapped"
)

// Entitlements is the resolved capability set for the current install. It
// is a value type — copy freely — and is safe for concurrent reads.
type Entitlements struct {
	// Plan is the resolved tier ("free", "crew", "constellation").
	Plan string
	// Seats is the licensed allowance (per-replica count in the mockup
	// model). 0 means unlimited / unset.
	Seats int
	// Features is the set of unlocked feature flags. Always non-nil.
	Features map[string]bool
}

// Has reports whether the given feature flag is unlocked. A zero-value
// Entitlements (nil Features) reports false for everything, so callers
// need not nil-check.
func (e Entitlements) Has(feature string) bool {
	return e.Features[feature]
}

// planFeatures is the canonical plan→feature-set map (the §1.4 table,
// reduced to the boolean feature flags relevant to self-hosted gating).
// Each tier is a superset of the one below it. This map is the single
// source of truth the rest of the system reads via FromLicense.
var planFeatures = map[string][]string{
	PlanFree: {
		// Explorer: the free single-binary build. Core CI/CD only, no
		// commercial feature flags.
	},
	PlanCrew: {
		FeatureMultiReplica,
		FeatureOIDCMFA,
		FeatureManagedSecrets,
		FeatureAuditOTLP,
	},
	PlanConstellation: {
		FeatureMultiReplica,
		FeatureOIDCMFA,
		FeatureManagedSecrets,
		FeatureAuditOTLP,
		FeatureSSOGroupMap,
		FeatureAirGapped,
	},
}

// Free returns the Explorer baseline entitlements — the degrade-to-Free
// target. It is what FromLicense returns when there is no active license.
func Free() Entitlements {
	return Entitlements{
		Plan:     PlanFree,
		Seats:    1, // Explorer is single-replica.
		Features: featureSet(PlanFree, nil),
	}
}

// FromLicense resolves a license into entitlements as of now. A nil or
// expired license degrades to Free (never an error). For an active
// license the plan's baseline feature set is unioned with any explicit
// per-license feature claims (so the vendor can grant a one-off feature
// without inventing a new plan). An unknown plan name still yields its
// explicit features on top of the Free baseline.
func FromLicense(l *model.License, now time.Time) Entitlements {
	if !l.IsActive(now) {
		return Free()
	}
	plan := l.Plan
	if plan == "" {
		plan = PlanFree
	}
	return Entitlements{
		Plan:     plan,
		Seats:    l.Seats,
		Features: featureSet(plan, l.Features),
	}
}

// featureSet builds the unlocked-feature map for a plan, unioning the
// plan's baseline with any extra explicitly-granted features. An unknown
// plan contributes no baseline (just the Free floor, which is empty) but
// still honours its explicit grants.
func featureSet(plan string, extra []string) map[string]bool {
	out := make(map[string]bool)
	for _, f := range planFeatures[plan] {
		out[f] = true
	}
	for _, f := range extra {
		if f != "" {
			out[f] = true
		}
	}
	return out
}
