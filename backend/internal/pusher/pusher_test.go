package pusher

import (
	"context"
	"testing"
)

func TestNoop(t *testing.T) {
	res, err := Noop{}.Push(context.Background(), Request{Target: "r/app:v1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Digest == "" {
		t.Error("Noop should return a non-empty Digest")
	}
}

func TestCrane_RejectsUnreachableSource(t *testing.T) {
	// Crane Push now uses go-containerregistry to resolve Source as a
	// remote ref. A reference to a non-routable host should fail —
	// the exact error wraps ErrUnavailable (parse) or a transport
	// error, depending on whether DNS resolution races vs ParseRef.
	_, err := NewCrane().Push(context.Background(), Request{
		Source: "127.0.0.1:1/missing/source:v1",
		Target: "127.0.0.1:1/missing/target:v1",
	})
	if err == nil {
		t.Fatal("expected error for unreachable refs")
	}
}

func TestDockerSock_RequiresTarget(t *testing.T) {
	_, err := NewDockerSock().Push(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestParseDigest(t *testing.T) {
	out := "The push refers to repository [registry/app]\nv1: digest: sha256:" +
		"deadbeef00000000000000000000000000000000000000000000000000000000 size: 1234\n"
	got := parseDigest(out)
	want := "sha256:deadbeef00000000000000000000000000000000000000000000000000000000"
	if got != want {
		t.Errorf("parseDigest: got %q want %q", got, want)
	}
	if parseDigest("no digest here") != "" {
		t.Error("parseDigest should return empty string when no match")
	}
}
