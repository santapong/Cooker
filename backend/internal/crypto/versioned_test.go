package crypto

import (
	"bytes"
	"testing"
)

func TestVersionedCiphertextPreservesLegacyWritesAndReaders(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCodec(key)
	otherKey, _ := GenerateKey()
	other, _ := NewCodec(otherKey)
	plain := []byte("synthetic-protected-value")
	legacy, _ := c.Seal(plain)
	if bytes.HasPrefix(legacy, envelopeMagic) {
		t.Fatal("legacy writer changed")
	}
	tagged, _ := c.WithVersionedWrites().Seal(plain)
	if !bytes.HasPrefix(tagged, envelopeMagic) {
		t.Fatal("tag missing")
	}
	for _, blob := range [][]byte{legacy, tagged} {
		got, err := c.Open(blob)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatal("reader round trip failed")
		}
		if _, err := other.Open(blob); err == nil {
			t.Fatal("wrong key accepted")
		}
	}
	stillLegacy, _ := c.Seal(plain)
	if bytes.HasPrefix(stillLegacy, envelopeMagic) {
		t.Fatal("opt-in mutated original writer")
	}
}

func TestVersionedCiphertextAuthenticatesAllHeaderFields(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCodec(key)
	blob, _ := c.SealVersioned([]byte("canary"))
	for i := range blob {
		modified := bytes.Clone(blob)
		modified[i] ^= 1
		if _, err := c.Open(modified); err == nil {
			t.Fatalf("modified byte %d accepted", i)
		}
	}
	for n := 0; n < len(blob); n++ {
		if _, err := c.Open(blob[:n]); err == nil {
			t.Fatalf("truncated length %d accepted", n)
		}
	}
	if bytes.Equal(c.RotationManifestMAC([]byte("one")), c.RotationManifestMAC([]byte("two"))) {
		t.Fatal("manifest not bound")
	}
	if bytes.Contains(blob, []byte(key)) {
		t.Fatal("key in envelope")
	}
}
