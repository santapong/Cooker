package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/license"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store/memory"
)

// TestKeygenSignVerifyRoundTrip exercises the vendor tool end to end on
// the real filesystem: keygen writes a keypair, sign mints a token from
// the private key, and the package verifier validates it against the
// generated public key — the same wire format the server consumes.
func TestKeygenSignVerifyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "lic")

	if err := cmdKeygen([]string{"-out", prefix}); err != nil {
		t.Fatalf("keygen: %v", err)
	}

	// keygen wrote <prefix>.key (private) and <prefix>.pub (public).
	priv, err := loadPrivateKey(prefix + ".key")
	if err != nil {
		t.Fatalf("load private key: %v", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("private key is %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	pubRaw, err := os.ReadFile(prefix + ".pub")
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	pub, err := license.DecodePublicKey(string(pubRaw))
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}

	// The public key written must be the verifier for the private key.
	if !pub.Equal(priv.Public().(ed25519.PublicKey)) {
		t.Fatalf("written public key does not match generated private key")
	}

	// Sign a token directly via the package (the same call cmdSign makes)
	// and verify it round-trips through the package verifier.
	now := time.Now()
	claims := &license.Claims{
		Plan:      "crew",
		Seats:     3,
		Customer:  "Round Trip Co",
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(720 * time.Hour).Unix(),
	}
	payload, err := license.MarshalClaims(claims)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	token := license.Encode(payload, ed25519.Sign(priv, payload))

	got, err := license.Verify(token, pub)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Plan != "crew" || got.Customer != "Round Trip Co" || got.Seats != 3 {
		t.Fatalf("verified claims mismatch: %+v", got)
	}
}

// TestParseExpiry covers the three accepted forms the sign subcommand takes.
func TestParseExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	if got, err := parseExpiry("0", now); err != nil || got != 0 {
		t.Fatalf("perpetual: got %d err %v, want 0", got, err)
	}
	if got, err := parseExpiry("", now); err != nil || got != 0 {
		t.Fatalf("empty perpetual: got %d err %v, want 0", got, err)
	}
	if got, err := parseExpiry("24h", now); err != nil || got != now.Add(24*time.Hour).Unix() {
		t.Fatalf("duration: got %d err %v", got, err)
	}
	rfc := "2030-01-02T15:04:05Z"
	want, _ := time.Parse(time.RFC3339, rfc)
	if got, err := parseExpiry(rfc, now); err != nil || got != want.Unix() {
		t.Fatalf("rfc3339: got %d err %v, want %d", got, err, want.Unix())
	}
	if _, err := parseExpiry("nonsense", now); err == nil {
		t.Fatalf("nonsense expiry should error")
	}
}

// TestSignedTokenInstallsViaLicenseService proves a token minted by the
// vendor tool's keypair installs through the real service.LicenseService
// using the generated public key (VerifyAny path), for both a perpetual
// license and one with a future expiry.
func TestSignedTokenInstallsViaLicenseService(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "svc")
	if err := cmdKeygen([]string{"-out", prefix}); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	priv, err := loadPrivateKey(prefix + ".key")
	if err != nil {
		t.Fatalf("load private key: %v", err)
	}
	pubRaw, err := os.ReadFile(prefix + ".pub")
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	pub, err := license.DecodePublicKey(string(pubRaw))
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}

	sign := func(c *license.Claims) string {
		payload, err := license.MarshalClaims(c)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return license.Encode(payload, ed25519.Sign(priv, payload))
	}

	now := time.Now()

	t.Run("perpetual", func(t *testing.T) {
		st := memory.New()
		svc := service.NewLicenseService(st.Licenses, []ed25519.PublicKey{pub})
		token := sign(&license.Claims{Plan: "constellation", Customer: "Perp Co", IssuedAt: now.Unix()})
		lic, err := svc.Install(t.Context(), token, "tester", "tester@x.com")
		if err != nil {
			t.Fatalf("install perpetual: %v", err)
		}
		if lic.ExpiresAt != nil {
			t.Errorf("perpetual license should have nil ExpiresAt, got %v", lic.ExpiresAt)
		}
		if lic.Plan != "constellation" {
			t.Errorf("plan = %q, want constellation", lic.Plan)
		}
		if lic.RawToken != token {
			t.Errorf("stored RawToken does not match signed token")
		}
	})

	t.Run("future expiry", func(t *testing.T) {
		st := memory.New()
		svc := service.NewLicenseService(st.Licenses, []ed25519.PublicKey{pub})
		exp := now.Add(8760 * time.Hour)
		token := sign(&license.Claims{Plan: "crew", Customer: "Expiry Co", IssuedAt: now.Unix(), ExpiresAt: exp.Unix()})
		lic, err := svc.Install(t.Context(), token, "tester", "tester@x.com")
		if err != nil {
			t.Fatalf("install with expiry: %v", err)
		}
		if lic.ExpiresAt == nil {
			t.Fatalf("expected non-nil ExpiresAt for a dated license")
		}
		if lic.ExpiresAt.Unix() != exp.Unix() {
			t.Errorf("ExpiresAt = %d, want %d", lic.ExpiresAt.Unix(), exp.Unix())
		}
	})

	t.Run("wrong key rejected", func(t *testing.T) {
		st := memory.New()
		// A different, unrelated public key must NOT verify the token.
		otherPub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		svc := service.NewLicenseService(st.Licenses, []ed25519.PublicKey{otherPub})
		token := sign(&license.Claims{Plan: "crew", Customer: "Acme", IssuedAt: now.Unix()})
		if _, err := svc.Install(t.Context(), token, "tester", "tester@x.com"); err == nil {
			t.Fatalf("install with wrong verifier key should fail")
		}
	})
}

// TestKeygenWritesExpectedEncoding pins the on-disk encoding the verify
// subcommand and the server's DecodePublicKey both rely on (std base64).
func TestKeygenWritesExpectedEncoding(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "enc")
	if err := cmdKeygen([]string{"-out", prefix}); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pubRaw, err := os.ReadFile(prefix + ".pub")
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	// The .pub file is std-base64 of a 32-byte key plus a trailing newline.
	decoded, err := base64.StdEncoding.DecodeString(trimNewline(string(pubRaw)))
	if err != nil {
		t.Fatalf("public key is not std base64: %v", err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		t.Fatalf("public key decodes to %d bytes, want %d", len(decoded), ed25519.PublicKeySize)
	}
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
