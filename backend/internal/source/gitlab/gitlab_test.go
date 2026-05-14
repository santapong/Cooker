package gitlab

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestPushEventBranch(t *testing.T) {
	cases := map[string]string{
		"refs/heads/main":          "main",
		"refs/heads/feature/login": "feature/login",
		"refs/tags/v1.0":           "",
		"":                         "",
	}
	for ref, want := range cases {
		if got := (PushEvent{Ref: ref}).Branch(); got != want {
			t.Errorf("Branch(%q)=%q want %q", ref, got, want)
		}
	}
}

func TestPushEventIsBranchDelete(t *testing.T) {
	del := PushEvent{Ref: "refs/heads/x", After: "0000000000000000000000000000000000000000"}
	if !del.IsBranchDelete() {
		t.Fatal("want branch-delete true")
	}
	normal := PushEvent{Ref: "refs/heads/x", After: "abcdef0123456789abcdef0123456789abcdef01"}
	if normal.IsBranchDelete() {
		t.Fatal("want branch-delete false")
	}
}

func TestPushEventFullName(t *testing.T) {
	jsonPayload := []byte(`{"object_kind":"push","ref":"refs/heads/main","after":"a","project":{"path_with_namespace":"group/sub/repo"}}`)
	var ev PushEvent
	if err := json.Unmarshal(jsonPayload, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.FullName() != "group/sub/repo" {
		t.Fatalf("FullName=%q", ev.FullName())
	}
}

func TestVerifyTokenMatch(t *testing.T) {
	if err := VerifyToken([]byte("shhh"), "shhh"); err != nil {
		t.Fatalf("matching token: %v", err)
	}
}

func TestVerifyTokenMismatch(t *testing.T) {
	err := VerifyToken([]byte("shhh"), "wrong")
	if !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("want ErrTokenMismatch, got %v", err)
	}
}

func TestVerifyTokenEmptyInputs(t *testing.T) {
	if err := VerifyToken(nil, "x"); err == nil {
		t.Fatal("want empty-secret error")
	}
	if err := VerifyToken([]byte("x"), ""); err == nil {
		t.Fatal("want missing-header error")
	}
}
