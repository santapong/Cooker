package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/entitlements"
	"github.com/santapong/cooker/internal/license"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func newLicenseService(t *testing.T) (*LicenseService, store.LicenseStore, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	st := memory.New().Licenses
	return NewLicenseService(st, []ed25519.PublicKey{pub}), st, pub, priv
}

func mintToken(t *testing.T, priv ed25519.PrivateKey, c *license.Claims) string {
	t.Helper()
	payload, err := license.MarshalClaims(c)
	if err != nil {
		t.Fatalf("MarshalClaims: %v", err)
	}
	return license.Encode(payload, ed25519.Sign(priv, payload))
}

func TestLicense_Install_Valid(t *testing.T) {
	svc, st, _, priv := newLicenseService(t)
	exp := time.Now().Add(365 * 24 * time.Hour).Unix()
	token := mintToken(t, priv, &license.Claims{
		Plan: entitlements.PlanCrew, Seats: 3, Customer: "ACME",
		IssuedAt: time.Now().Unix(), ExpiresAt: exp,
	})

	lic, err := svc.Install(context.Background(), token, "admin-sub", "admin@x.com")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if lic.Plan != entitlements.PlanCrew || lic.Seats != 3 || lic.Customer != "ACME" {
		t.Errorf("claims not mapped onto license: %+v", lic)
	}
	if lic.InstalledBySub != "admin-sub" || lic.InstalledByEmail != "admin@x.com" {
		t.Errorf("installer identity not recorded: %+v", lic)
	}
	if lic.RawToken != token {
		t.Errorf("raw token should be persisted for re-verification")
	}
	// Persisted as the active license.
	got, err := st.GetActive(context.Background())
	if err != nil || got.ID != lic.ID {
		t.Errorf("license not active in store: %v", err)
	}
}

func TestLicense_Install_RejectsExpired(t *testing.T) {
	svc, _, _, priv := newLicenseService(t)
	token := mintToken(t, priv, &license.Claims{
		Plan: entitlements.PlanCrew, ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	})
	_, err := svc.Install(context.Background(), token, "s", "e")
	if !errors.Is(err, ErrLicenseExpired) {
		t.Fatalf("expected ErrLicenseExpired, got %v", err)
	}
}

func TestLicense_Install_RejectsInvalidSignature(t *testing.T) {
	svc, _, _, _ := newLicenseService(t)
	// Sign with a DIFFERENT key than the service trusts.
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	token := mintToken(t, otherPriv, &license.Claims{Plan: entitlements.PlanCrew})
	_, err := svc.Install(context.Background(), token, "s", "e")
	if !errors.Is(err, ErrLicenseInvalid) {
		t.Fatalf("expected ErrLicenseInvalid, got %v", err)
	}
}

func TestLicense_Install_RejectsEmpty(t *testing.T) {
	svc, _, _, _ := newLicenseService(t)
	if _, err := svc.Install(context.Background(), "  ", "s", "e"); !errors.Is(err, ErrLicenseEmpty) {
		t.Fatalf("expected ErrLicenseEmpty, got %v", err)
	}
}

func TestLicense_Install_VerificationDisabledWhenNoKey(t *testing.T) {
	st := memory.New().Licenses
	svc := NewLicenseService(st, nil) // no public key
	if _, err := svc.Install(context.Background(), "x.y", "s", "e"); !errors.Is(err, ErrVerificationDisabled) {
		t.Fatalf("expected ErrVerificationDisabled, got %v", err)
	}
}

// TestLicense_Install_RotationAcceptsSecondKey simulates a vendor key
// rollover: the service trusts two keys, and a token signed by the SECOND
// key installs successfully. This is the W3 rotation guarantee.
func TestLicense_Install_RotationAcceptsSecondKey(t *testing.T) {
	pub1, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey key1: %v", err)
	}
	pub2, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey key2: %v", err)
	}
	st := memory.New().Licenses
	svc := NewLicenseService(st, []ed25519.PublicKey{pub1, pub2})

	// Token signed with key#2 — the "incoming" key during rotation.
	token := mintToken(t, priv2, &license.Claims{
		Plan: entitlements.PlanCrew, Seats: 5, Customer: "ROTATED",
		IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	lic, err := svc.Install(context.Background(), token, "s", "e")
	if err != nil {
		t.Fatalf("Install with second key should succeed: %v", err)
	}
	if lic.Customer != "ROTATED" || lic.Seats != 5 {
		t.Errorf("claims not mapped: %+v", lic)
	}

	// A token signed by a key in NEITHER set still fails.
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	bad := mintToken(t, otherPriv, &license.Claims{Plan: entitlements.PlanCrew})
	if _, err := svc.Install(context.Background(), bad, "s", "e"); !errors.Is(err, ErrLicenseInvalid) {
		t.Fatalf("expected ErrLicenseInvalid for untrusted key, got %v", err)
	}
}

func TestLicense_Current_NoneIsFreeNotError(t *testing.T) {
	svc, _, _, _ := newLicenseService(t)
	lic, ent, err := svc.Current(context.Background())
	if err != nil {
		t.Fatalf("Current with no license should not error: %v", err)
	}
	if lic != nil {
		t.Errorf("expected nil license, got %+v", lic)
	}
	if ent.Plan != entitlements.PlanFree {
		t.Errorf("expected Free entitlements, got %q", ent.Plan)
	}
}

func TestLicense_Current_AfterInstall(t *testing.T) {
	svc, _, _, priv := newLicenseService(t)
	token := mintToken(t, priv, &license.Claims{
		Plan: entitlements.PlanConstellation, Seats: 0,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if _, err := svc.Install(context.Background(), token, "s", "e"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	lic, ent, err := svc.Current(context.Background())
	if err != nil || lic == nil {
		t.Fatalf("Current: lic=%v err=%v", lic, err)
	}
	if ent.Plan != entitlements.PlanConstellation {
		t.Errorf("expected constellation entitlements, got %q", ent.Plan)
	}
	if !ent.Has(entitlements.FeatureSSOGroupMap) {
		t.Errorf("constellation should unlock sso_group_map")
	}
}

func TestLicense_Remove_Idempotent(t *testing.T) {
	svc, _, _, priv := newLicenseService(t)
	token := mintToken(t, priv, &license.Claims{Plan: entitlements.PlanCrew, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if _, err := svc.Install(context.Background(), token, "s", "e"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := svc.Remove(context.Background()); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// Removing again is a no-op.
	if err := svc.Remove(context.Background()); err != nil {
		t.Fatalf("Remove (idempotent): %v", err)
	}
	_, ent, _ := svc.Current(context.Background())
	if ent.Plan != entitlements.PlanFree {
		t.Errorf("after remove expected Free, got %q", ent.Plan)
	}
}

func TestLicense_Entitlements_NeverErrors(t *testing.T) {
	svc, _, _, _ := newLicenseService(t)
	if ent := svc.Entitlements(context.Background()); ent.Plan != entitlements.PlanFree {
		t.Errorf("expected Free, got %q", ent.Plan)
	}
}
