package pusher

import (
	"bytes"
	"context"
	"strings"
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

// TestNoop_WritesLogLine locks the LogWriter contract: every Pusher
// implementation, including the no-op, writes the canonical "Pushed
// image to <ref>" line so the run page renders something for the
// stage. Closes dag-performance.md §4 High #2 (push half).
func TestNoop_WritesLogLine(t *testing.T) {
	var buf bytes.Buffer
	_, err := Noop{}.Push(context.Background(), Request{Target: "r/app:v1", LogWriter: &buf})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "Pushed image to r/app:v1") {
		t.Errorf("Noop LogWriter output = %q, want it to contain 'Pushed image to r/app:v1'", got)
	}
}

// TestNoop_NilLogWriterIsSafe — adapters must tolerate a nil
// LogWriter (tests, direct callers).
func TestNoop_NilLogWriterIsSafe(t *testing.T) {
	_, err := Noop{}.Push(context.Background(), Request{Target: "r/app:v1"})
	if err != nil {
		t.Fatalf("nil LogWriter must not error: %v", err)
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
