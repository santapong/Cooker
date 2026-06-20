package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func encodeStd(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// signClaims is a test helper mirroring what the vendor's signing CLI
// does: marshal the claims canonically, sign the raw bytes, frame them.
func signClaims(t *testing.T, priv ed25519.PrivateKey, c *Claims) string {
	t.Helper()
	payload, err := MarshalClaims(c)
	if err != nil {
		t.Fatalf("MarshalClaims: %v", err)
	}
	sig := ed25519.Sign(priv, payload)
	return Encode(payload, sig)
}

func newKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func sampleClaims() *Claims {
	return &Claims{
		Plan:      "crew",
		Seats:     5,
		Features:  []string{"oidc", "managed-secrets"},
		Customer:  "ACME Corp",
		IssuedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
		ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
	}
}

func TestVerify_Valid(t *testing.T) {
	pub, priv := newKeypair(t)
	in := sampleClaims()
	token := signClaims(t, priv, in)

	got, err := Verify(token, pub)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Plan != in.Plan || got.Seats != in.Seats || got.Customer != in.Customer {
		t.Errorf("claims roundtrip mismatch: got %+v want %+v", got, in)
	}
	if len(got.Features) != 2 || got.Features[0] != "oidc" {
		t.Errorf("features not preserved: %v", got.Features)
	}
	if got.ExpiresAtTime() == nil || got.ExpiresAtTime().Unix() != in.ExpiresAt {
		t.Errorf("expiry not preserved: %v", got.ExpiresAtTime())
	}
}

func TestVerify_PerpetualHasNilExpiry(t *testing.T) {
	pub, priv := newKeypair(t)
	in := sampleClaims()
	in.ExpiresAt = 0 // perpetual
	token := signClaims(t, priv, in)

	got, err := Verify(token, pub)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.ExpiresAtTime() != nil {
		t.Errorf("perpetual license should have nil expiry, got %v", got.ExpiresAtTime())
	}
	if got.IsExpired(time.Now()) {
		t.Errorf("perpetual license must never be expired")
	}
}

func TestVerify_TamperedPayload(t *testing.T) {
	pub, priv := newKeypair(t)
	token := signClaims(t, priv, sampleClaims())

	// Re-sign a different payload but keep the original signature: flip
	// the payload segment to a forged one.
	forged := signClaims(t, priv, &Claims{Plan: "constellation", Seats: 999})
	parts := strings.Split(token, ".")
	forgedParts := strings.Split(forged, ".")
	tampered := forgedParts[0] + "." + parts[1] // forged payload, original sig

	_, err := Verify(tampered, pub)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("expected ErrBadSignature for tampered payload, got %v", err)
	}
}

func TestVerify_WrongKey(t *testing.T) {
	_, priv := newKeypair(t)
	otherPub, _ := newKeypair(t)
	token := signClaims(t, priv, sampleClaims())

	_, err := Verify(token, otherPub)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("expected ErrBadSignature for wrong key, got %v", err)
	}
}

func TestVerify_Malformed(t *testing.T) {
	pub, _ := newKeypair(t)
	cases := map[string]string{
		"empty":         "",
		"one segment":   "abcdef",
		"three segment": "a.b.c",
		"empty payload": ".sig",
		"empty sig":     "payload.",
		"bad b64":       "!!!.@@@",
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Verify(tok, pub); !errors.Is(err, ErrMalformed) {
				t.Errorf("expected ErrMalformed, got %v", err)
			}
		})
	}
}

func TestVerify_ShortSignature(t *testing.T) {
	pub, priv := newKeypair(t)
	payload, _ := MarshalClaims(sampleClaims())
	_ = priv
	// Frame a valid payload with a too-short signature.
	tok := Encode(payload, []byte("too-short"))
	if _, err := Verify(tok, pub); !errors.Is(err, ErrMalformed) {
		t.Errorf("expected ErrMalformed for short signature, got %v", err)
	}
}

func TestVerify_BadKey(t *testing.T) {
	token := "anything.here"
	if _, err := Verify(token, ed25519.PublicKey([]byte("short"))); !errors.Is(err, ErrBadKey) {
		t.Errorf("expected ErrBadKey, got %v", err)
	}
}

func TestDecodePublicKey(t *testing.T) {
	pub, _ := newKeypair(t)
	// Std base64
	std := encodeStd(pub)
	got, err := DecodePublicKey(std)
	if err != nil {
		t.Fatalf("DecodePublicKey(std): %v", err)
	}
	if !got.Equal(pub) {
		t.Errorf("std roundtrip mismatch")
	}
	// URL base64 (no padding)
	url := b64.EncodeToString(pub)
	got2, err := DecodePublicKey(url)
	if err != nil {
		t.Fatalf("DecodePublicKey(url): %v", err)
	}
	if !got2.Equal(pub) {
		t.Errorf("url roundtrip mismatch")
	}
	// Errors
	if _, err := DecodePublicKey(""); !errors.Is(err, ErrBadKey) {
		t.Errorf("empty: expected ErrBadKey, got %v", err)
	}
	if _, err := DecodePublicKey("not-base64-!!"); !errors.Is(err, ErrBadKey) {
		t.Errorf("garbage: expected ErrBadKey, got %v", err)
	}
	if _, err := DecodePublicKey(b64.EncodeToString([]byte("short"))); !errors.Is(err, ErrBadKey) {
		t.Errorf("short: expected ErrBadKey, got %v", err)
	}
}

func TestVerifyAny_EmptyKeysIsBadKey(t *testing.T) {
	_, priv := newKeypair(t)
	token := signClaims(t, priv, sampleClaims())
	if _, err := VerifyAny(token, nil); !errors.Is(err, ErrBadKey) {
		t.Fatalf("empty keys: expected ErrBadKey, got %v", err)
	}
}

func TestVerifyAny_FirstKeyMatches(t *testing.T) {
	pub1, priv1 := newKeypair(t)
	pub2, _ := newKeypair(t)
	token := signClaims(t, priv1, sampleClaims())
	claims, err := VerifyAny(token, []ed25519.PublicKey{pub1, pub2})
	if err != nil {
		t.Fatalf("VerifyAny: %v", err)
	}
	if claims.Customer != "ACME Corp" {
		t.Errorf("claims not decoded: %+v", claims)
	}
}

func TestVerifyAny_SecondKeyMatches_Rotation(t *testing.T) {
	pub1, _ := newKeypair(t)     // outgoing key, no longer the signer
	pub2, priv2 := newKeypair(t) // incoming key
	token := signClaims(t, priv2, sampleClaims())
	claims, err := VerifyAny(token, []ed25519.PublicKey{pub1, pub2})
	if err != nil {
		t.Fatalf("VerifyAny with second key should succeed: %v", err)
	}
	if claims.Seats != 5 {
		t.Errorf("claims not decoded: %+v", claims)
	}
}

func TestVerifyAny_NoKeyMatchesIsBadSignature(t *testing.T) {
	pub1, _ := newKeypair(t)
	pub2, _ := newKeypair(t)
	_, signer := newKeypair(t) // signed by an untrusted key
	token := signClaims(t, signer, sampleClaims())
	if _, err := VerifyAny(token, []ed25519.PublicKey{pub1, pub2}); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("expected ErrBadSignature, got %v", err)
	}
}

func TestVerifyAny_SkipsUnusableKeys(t *testing.T) {
	good, priv := newKeypair(t)
	short := ed25519.PublicKey([]byte("too-short")) // wrong length, unusable
	token := signClaims(t, priv, sampleClaims())
	// The unusable key must be skipped, the good key must still verify.
	if _, err := VerifyAny(token, []ed25519.PublicKey{short, good}); err != nil {
		t.Fatalf("VerifyAny should skip the unusable key and verify: %v", err)
	}
	// All keys unusable -> ErrBadKey.
	if _, err := VerifyAny(token, []ed25519.PublicKey{short}); !errors.Is(err, ErrBadKey) {
		t.Fatalf("all-unusable keys: expected ErrBadKey, got %v", err)
	}
}

func TestVerifyAny_MalformedTokenSurfacesMalformed(t *testing.T) {
	pub, _ := newKeypair(t)
	if _, err := VerifyAny("not-a-valid-token", []ed25519.PublicKey{pub}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("expected ErrMalformed, got %v", err)
	}
}
