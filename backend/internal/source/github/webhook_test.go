package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func signatureFor(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature_Valid(t *testing.T) {
	secret := []byte("shh")
	body := []byte(`{"ref":"refs/heads/main"}`)
	sig := signatureFor(secret, body)
	if err := VerifySignature(secret, body, sig); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestVerifySignature_TamperedBody(t *testing.T) {
	secret := []byte("shh")
	body := []byte(`{"ref":"refs/heads/main"}`)
	sig := signatureFor(secret, body)
	err := VerifySignature(secret, []byte(`{"ref":"refs/heads/evil"}`), sig)
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("expected ErrBadSignature, got %v", err)
	}
}

func TestVerifySignature_WrongSecret(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	sig := signatureFor([]byte("wrong"), body)
	err := VerifySignature([]byte("right"), body, sig)
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("expected ErrBadSignature, got %v", err)
	}
}

func TestVerifySignature_MissingPrefix(t *testing.T) {
	err := VerifySignature([]byte("s"), []byte("b"), "deadbeef")
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("expected ErrBadSignature, got %v", err)
	}
}

func TestVerifySignature_InvalidHex(t *testing.T) {
	err := VerifySignature([]byte("s"), []byte("b"), "sha256=not-hex")
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("expected ErrBadSignature, got %v", err)
	}
}

func TestVerifySignature_EmptySecret(t *testing.T) {
	if err := VerifySignature(nil, []byte("b"), "sha256=00"); err == nil {
		t.Error("expected error on empty secret")
	}
}

func TestPushEvent_Branch(t *testing.T) {
	p := &PushEvent{Ref: "refs/heads/feature/x"}
	if got := p.Branch(); got != "feature/x" {
		t.Errorf("got %q", got)
	}
	// Tag pushes have a different prefix; Branch should return empty.
	p = &PushEvent{Ref: "refs/tags/v1.0.0"}
	if got := p.Branch(); got != "" {
		t.Errorf("tag ref should yield empty branch; got %q", got)
	}
}
