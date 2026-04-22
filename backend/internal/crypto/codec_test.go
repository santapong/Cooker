package crypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestCodec_SealOpen_RoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Active() {
		t.Fatal("codec should be active with a key")
	}

	plain := []byte("super-secret-token")
	sealed, err := c.Seal(plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(sealed, plain) {
		t.Error("sealed output contains plaintext (not encrypted?)")
	}
	opened, err := c.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(opened, plain) {
		t.Errorf("round-trip mismatch: got %q want %q", opened, plain)
	}
}

func TestCodec_TamperDetected(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCodec(key)
	sealed, _ := c.Seal([]byte("hello"))
	sealed[len(sealed)-1] ^= 0x01 // flip a bit in the tag
	if _, err := c.Open(sealed); err == nil {
		t.Error("expected open to fail on tampered ciphertext")
	}
}

func TestCodec_NoKey(t *testing.T) {
	c, err := NewCodec("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Active() {
		t.Error("codec with empty key should not be active")
	}
	if _, err := c.Seal([]byte("x")); !errors.Is(err, ErrNoKey) {
		t.Errorf("expected ErrNoKey, got %v", err)
	}
	if _, err := c.Open([]byte("x")); !errors.Is(err, ErrNoKey) {
		t.Errorf("expected ErrNoKey, got %v", err)
	}
}

func TestCodec_BadKey(t *testing.T) {
	if _, err := NewCodec("not-base64!"); err == nil {
		t.Error("expected error on non-base64 key")
	}
	// 16-byte key, not 32.
	if _, err := NewCodec("AAAAAAAAAAAAAAAAAAAAAA=="); err == nil {
		t.Error("expected error on short key")
	}
}

func TestCodec_NonceDifferPerCall(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCodec(key)
	a, _ := c.Seal([]byte("same"))
	b, _ := c.Seal([]byte("same"))
	if bytes.Equal(a, b) {
		t.Error("sealing the same plaintext twice should produce different ciphertext (random nonces)")
	}
}
