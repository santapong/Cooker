package notifier

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/santapong/cooker/internal/crypto"
)

// The seal/open round-trip is pure (no DB), so it's tested against a
// bare PostgresTargetStore carrying only a codec.

func newSealingStore(t *testing.T) *PostgresTargetStore {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	return (&PostgresTargetStore{}).WithCodec(codec)
}

func TestSealOpenConfig_RoundTrip(t *testing.T) {
	s := newSealingStore(t)
	plain := []byte(`{"webhookUrl":"https://hooks.example.com/T00/B00/xyz","password":"hunter2"}`)

	sealed, err := s.sealConfig(plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	// The stored value must be valid JSON (JSONB column) and must NOT
	// contain the plaintext secret.
	if !json.Valid(sealed) {
		t.Fatal("sealed config is not valid JSON")
	}
	if bytes.Contains(sealed, []byte("hunter2")) || bytes.Contains(sealed, []byte("hooks.example.com")) {
		t.Fatalf("sealed config leaks plaintext: %s", sealed)
	}

	opened, err := s.openConfig(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(opened) != string(plain) {
		t.Errorf("round-trip mismatch: got %s want %s", opened, plain)
	}
}

func TestOpenConfig_LegacyPlaintextPassesThrough(t *testing.T) {
	s := newSealingStore(t)
	// A row written before encryption: raw channel JSON, no envelope.
	legacy := []byte(`{"webhookUrl":"https://old.example.com/x"}`)
	opened, err := s.openConfig(legacy)
	if err != nil {
		t.Fatalf("legacy open: %v", err)
	}
	if string(opened) != string(legacy) {
		t.Errorf("legacy plaintext must pass through unchanged, got %s", opened)
	}
}

func TestSealConfig_NoCodecIsPlaintext(t *testing.T) {
	s := &PostgresTargetStore{} // no codec
	plain := []byte(`{"a":1}`)
	sealed, err := s.sealConfig(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) != string(plain) {
		t.Errorf("without a codec, config must be stored as-is; got %s", sealed)
	}
}

func TestOpenConfig_EncryptedWithoutCodecErrors(t *testing.T) {
	// Seal with a codec, then attempt to open with none — the row is
	// encrypted but the operator dropped COOKER_SECRET_KEY. Must error,
	// not silently hand back the envelope as if it were config.
	sealed, err := newSealingStore(t).sealConfig([]byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&PostgresTargetStore{}).openConfig(sealed); err == nil {
		t.Fatal("expected an error opening an encrypted config without a codec")
	}
}
