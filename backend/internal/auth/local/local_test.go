package local

import (
	"strings"
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("hunter2-secure")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash == "" {
		t.Fatalf("expected non-empty hash")
	}
	if err := VerifyPassword(hash, "hunter2-secure"); err != nil {
		t.Fatalf("expected valid password to verify, got: %v", err)
	}
	if err := VerifyPassword(hash, "wrong"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for wrong password, got: %v", err)
	}
}

func TestHashPasswordTooShort(t *testing.T) {
	_, err := HashPassword("short")
	if err != ErrPasswordTooShort {
		t.Fatalf("expected ErrPasswordTooShort, got: %v", err)
	}
}

func TestNormaliseEmail(t *testing.T) {
	cases := []struct {
		in, out string
		err     error
	}{
		{"  Foo@Example.com ", "foo@example.com", nil},
		{"BAR@example.com", "bar@example.com", nil},
		{"", "", ErrEmailRequired},
		{"  ", "", ErrEmailRequired},
		{"no-at-sign", "", ErrEmailInvalid},
		{"@no-local", "", ErrEmailInvalid},
		{"no-domain@", "", ErrEmailInvalid},
	}
	for _, tc := range cases {
		got, err := NormaliseEmail(tc.in)
		if got != tc.out || err != tc.err {
			t.Errorf("NormaliseEmail(%q) = (%q, %v); want (%q, %v)", tc.in, got, err, tc.out, tc.err)
		}
	}
}

func TestNewIssuerRejectsShortKey(t *testing.T) {
	_, err := NewIssuer([]byte("short"), 0)
	if err != ErrSigningKeyTooShort {
		t.Fatalf("expected ErrSigningKeyTooShort, got: %v", err)
	}
}

func TestIssueAndVerify(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	iss, err := NewIssuer(key, time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	tok, exp, err := iss.Issue("user-1", "user@example.com", "User One", "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok == "" {
		t.Fatalf("expected non-empty token")
	}
	if time.Until(exp) > time.Minute || time.Until(exp) < 30*time.Second {
		t.Fatalf("unexpected expiry: %v", exp)
	}
	claims, err := iss.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user-1" || claims.Email != "user@example.com" || claims.Role != "admin" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	key := make([]byte, 32)
	iss, _ := NewIssuer(key, time.Minute)
	tok, _, _ := iss.Issue("user-1", "u@e.com", "U", "viewer")

	tampered := tok + "x"
	if _, err := iss.Verify(tampered); err != ErrTokenInvalid {
		t.Fatalf("expected ErrTokenInvalid for tampered token, got: %v", err)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = 0xff
	}
	iss1, _ := NewIssuer(key1, time.Minute)
	iss2, _ := NewIssuer(key2, time.Minute)
	tok, _, _ := iss1.Issue("user-1", "u@e.com", "U", "viewer")
	if _, err := iss2.Verify(tok); err != ErrTokenInvalid {
		t.Fatalf("expected ErrTokenInvalid for token signed with other key, got: %v", err)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	key := make([]byte, 32)
	iss, _ := NewIssuer(key, time.Nanosecond)
	tok, _, _ := iss.Issue("user-1", "u@e.com", "U", "viewer")
	time.Sleep(2 * time.Millisecond)
	if _, err := iss.Verify(tok); err != ErrTokenInvalid {
		t.Fatalf("expected ErrTokenInvalid for expired token, got: %v", err)
	}
}

func TestLooksLikeLocalToken(t *testing.T) {
	key := make([]byte, 32)
	iss, _ := NewIssuer(key, time.Minute)
	tok, _, _ := iss.Issue("u", "e@x.com", "n", "admin")
	if !LooksLikeLocalToken(tok) {
		t.Fatalf("expected true for cooker-local token")
	}
	if LooksLikeLocalToken("not.a.jwt") {
		t.Fatalf("expected false for non-jwt")
	}
	if LooksLikeLocalToken("") {
		t.Fatalf("expected false for empty")
	}
	// JWT with a different issuer (constructed by hand-ish via the
	// header.payload.sig structure — easier to assert on shape).
	if LooksLikeLocalToken("eyJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJzb21lYm9keS1lbHNlIn0.X") {
		t.Fatalf("expected false for non-cooker-local JWT")
	}
}

func TestDefaultTTLApplied(t *testing.T) {
	key := make([]byte, 32)
	iss, _ := NewIssuer(key, 0)
	if iss.ttl != DefaultTokenTTL {
		t.Fatalf("expected DefaultTokenTTL when 0 passed, got %v", iss.ttl)
	}
}

func TestErrPasswordTooShortMessage(t *testing.T) {
	if !strings.Contains(ErrPasswordTooShort.Error(), "8") {
		t.Fatalf("ErrPasswordTooShort should mention min length: %v", ErrPasswordTooShort)
	}
}
