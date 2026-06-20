package entitlements

import (
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func TestFromLicense_NilDegradesToFree(t *testing.T) {
	e := FromLicense(nil, time.Now())
	if e.Plan != PlanFree {
		t.Errorf("nil license should degrade to Free, got plan %q", e.Plan)
	}
	if e.Has(FeatureOIDCMFA) {
		t.Errorf("Free must not have %q", FeatureOIDCMFA)
	}
	if e.Features == nil {
		t.Errorf("Features must never be nil")
	}
}

func TestFromLicense_ExpiredDegradesToFree(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	l := &model.License{
		Plan:      PlanConstellation,
		Seats:     100,
		ExpiresAt: &past,
	}
	e := FromLicense(l, time.Now())
	if e.Plan != PlanFree {
		t.Errorf("expired license should degrade to Free, got %q", e.Plan)
	}
	if e.Has(FeatureSSOGroupMap) {
		t.Errorf("expired Constellation must not retain %q", FeatureSSOGroupMap)
	}
	if e.Seats != 1 {
		t.Errorf("Free baseline seats should be 1, got %d", e.Seats)
	}
}

func TestFromLicense_ActiveCrew(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	l := &model.License{Plan: PlanCrew, Seats: 3, ExpiresAt: &future}
	e := FromLicense(l, time.Now())
	if e.Plan != PlanCrew {
		t.Errorf("got plan %q want crew", e.Plan)
	}
	if e.Seats != 3 {
		t.Errorf("got seats %d want 3", e.Seats)
	}
	for _, f := range []string{FeatureMultiReplica, FeatureOIDCMFA, FeatureManagedSecrets, FeatureAuditOTLP} {
		if !e.Has(f) {
			t.Errorf("Crew should unlock %q", f)
		}
	}
	if e.Has(FeatureSSOGroupMap) {
		t.Errorf("Crew must NOT unlock Constellation-only %q", FeatureSSOGroupMap)
	}
}

func TestFromLicense_PerpetualConstellation(t *testing.T) {
	l := &model.License{Plan: PlanConstellation, Seats: 0, ExpiresAt: nil}
	e := FromLicense(l, time.Now())
	if e.Plan != PlanConstellation {
		t.Errorf("got plan %q want constellation", e.Plan)
	}
	if e.Seats != 0 {
		t.Errorf("unlimited seats should be 0, got %d", e.Seats)
	}
	for _, f := range []string{FeatureSSOGroupMap, FeatureAirGapped, FeatureAuditOTLP} {
		if !e.Has(f) {
			t.Errorf("Constellation should unlock %q", f)
		}
	}
}

func TestFromLicense_ExplicitFeatureGrant(t *testing.T) {
	// A Crew license that additionally grants a Constellation feature.
	l := &model.License{
		Plan:     PlanCrew,
		Features: []string{FeatureSSOGroupMap, ""}, // empty filtered out
	}
	e := FromLicense(l, time.Now())
	if !e.Has(FeatureSSOGroupMap) {
		t.Errorf("explicit grant of %q should be honoured", FeatureSSOGroupMap)
	}
	if !e.Has(FeatureOIDCMFA) {
		t.Errorf("plan baseline %q should remain", FeatureOIDCMFA)
	}
}

func TestFromLicense_UnknownPlanFallsToBaselineFloor(t *testing.T) {
	l := &model.License{Plan: "enterprise-plus", Features: []string{"some_feature"}}
	e := FromLicense(l, time.Now())
	if e.Plan != "enterprise-plus" {
		t.Errorf("plan name should pass through, got %q", e.Plan)
	}
	if !e.Has("some_feature") {
		t.Errorf("explicit feature should be honoured even on unknown plan")
	}
	if e.Has(FeatureOIDCMFA) {
		t.Errorf("unknown plan should not get crew baseline implicitly")
	}
}

func TestHas_ZeroValueSafe(t *testing.T) {
	var e Entitlements
	if e.Has(FeatureOIDCMFA) {
		t.Errorf("zero-value Entitlements.Has must be false")
	}
}
