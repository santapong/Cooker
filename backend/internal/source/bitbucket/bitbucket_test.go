package bitbucket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func TestPushEventBranchAndDelete(t *testing.T) {
	payload := []byte(`{
	  "eventKey":"repo:refs_changed",
	  "changes":[{"ref":{"id":"refs/heads/main","displayId":"main","type":"BRANCH"},"type":"UPDATE","toHash":"abc"}],
	  "repository":{"slug":"web","project":{"key":"COOK"}}
	}`)
	var ev PushEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.FullName() != "COOK/web" {
		t.Fatalf("FullName=%q", ev.FullName())
	}
	if ev.Branch() != "main" {
		t.Fatalf("Branch=%q", ev.Branch())
	}
	if ev.IsBranchDelete() {
		t.Fatal("want delete=false")
	}
}

func TestPushEventBranchDelete(t *testing.T) {
	ev := PushEvent{Changes: []struct {
		Ref struct {
			ID        string `json:"id"`
			DisplayID string `json:"displayId"`
			Type      string `json:"type"`
		} `json:"ref"`
		ToHash   string `json:"toHash"`
		FromHash string `json:"fromHash"`
		Type     string `json:"type"`
	}{{Type: "DELETE"}}}
	ev.Changes[0].Ref.Type = "BRANCH"
	ev.Changes[0].Ref.DisplayID = "feature/x"
	if !ev.IsBranchDelete() {
		t.Fatal("want delete=true")
	}
}

func TestVerifySignatureMatch(t *testing.T) {
	secret := []byte("shhh")
	body := []byte(`{"hello":"world"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if err := VerifySignature(secret, body, sig); err != nil {
		t.Fatalf("valid sig: %v", err)
	}
}

func TestVerifySignatureMismatch(t *testing.T) {
	sig := "sha256=" + hex.EncodeToString([]byte("deadbeef"))
	err := VerifySignature([]byte("shhh"), []byte("body"), sig)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("want ErrBadSignature, got %v", err)
	}
}

func TestVerifySignatureMalformedInputs(t *testing.T) {
	if err := VerifySignature([]byte("s"), []byte("b"), ""); err == nil {
		t.Fatal("want missing-header error")
	}
	if err := VerifySignature([]byte("s"), []byte("b"), "md5=xxx"); err == nil {
		t.Fatal("want prefix-error")
	}
	if err := VerifySignature([]byte("s"), []byte("b"), "sha256=not-hex"); err == nil {
		t.Fatal("want hex-decode error")
	}
}
