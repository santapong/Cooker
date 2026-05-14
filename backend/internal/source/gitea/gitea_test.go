package gitea

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func TestPushEventBranchAndFullName(t *testing.T) {
	payload := []byte(`{"ref":"refs/heads/main","after":"abc","repository":{"full_name":"team/cooker"}}`)
	var ev PushEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Branch() != "main" {
		t.Fatalf("Branch=%q", ev.Branch())
	}
	if ev.FullName() != "team/cooker" {
		t.Fatalf("FullName=%q", ev.FullName())
	}
}

func TestPushEventIsBranchDelete(t *testing.T) {
	if !(PushEvent{Deleted: true}).IsBranchDelete() {
		t.Fatal("explicit deleted flag should mark delete")
	}
	if !(PushEvent{After: "0000000000000000000000000000000000000000"}).IsBranchDelete() {
		t.Fatal("zero-hash after should mark delete")
	}
	if (PushEvent{After: "abc"}).IsBranchDelete() {
		t.Fatal("normal push should not mark delete")
	}
}

func TestVerifySignatureMatch(t *testing.T) {
	secret := []byte("shhh")
	body := []byte(`{"x":1}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	if err := VerifySignature(secret, body, sig); err != nil {
		t.Fatalf("valid sig: %v", err)
	}
}

func TestVerifySignatureMismatch(t *testing.T) {
	err := VerifySignature([]byte("shhh"), []byte("body"), "deadbeef00")
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("want ErrBadSignature, got %v", err)
	}
}

func TestVerifySignatureMalformedInputs(t *testing.T) {
	if err := VerifySignature([]byte("s"), []byte("b"), ""); err == nil {
		t.Fatal("want missing-header error")
	}
	if err := VerifySignature([]byte("s"), []byte("b"), "not-hex"); err == nil {
		t.Fatal("want hex-decode error")
	}
}
