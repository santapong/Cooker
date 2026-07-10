package deployer

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNoop(t *testing.T) {
	_, err := Noop{}.Deploy(context.Background(), Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestNoop_WritesLogLine locks the LogWriter contract for the deploy
// half of T2: every Deployer implementation, including the no-op,
// writes a canonical "Applied ..." line. Closes
// dag-performance.md §4 High #2 (deploy half).
func TestNoop_WritesLogLine(t *testing.T) {
	var buf bytes.Buffer
	_, err := Noop{}.Deploy(context.Background(), Request{Kind: KindManifest, LogWriter: &buf})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "Applied") {
		t.Errorf("Noop LogWriter output = %q, want it to contain 'Applied'", got)
	}
}

func TestNoop_NilLogWriterIsSafe(t *testing.T) {
	_, err := Noop{}.Deploy(context.Background(), Request{Kind: KindManifest})
	if err != nil {
		t.Fatalf("nil LogWriter must not error: %v", err)
	}
}

func TestClientGo_Unwired(t *testing.T) {
	_, err := NewClientGo("").Deploy(context.Background(), Request{Kind: KindManifest, Manifest: []byte("apiVersion: v1\nkind: ConfigMap")})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestKubectl_RejectsHelm(t *testing.T) {
	_, err := NewKubectl().Deploy(context.Background(), Request{Kind: KindHelm, HelmChart: "./chart"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable for helm kind, got %v", err)
	}
}

func TestKubectl_RequiresManifest(t *testing.T) {
	_, err := NewKubectl().Deploy(context.Background(), Request{Kind: KindManifest})
	if err == nil {
		t.Fatal("expected validation error for empty manifest")
	}
}

func TestParseAppliedResources(t *testing.T) {
	out := `deployment.apps/web created
service/web unchanged
configmap/web-config configured`
	got := parseAppliedResources(out)
	want := []string{"deployment.apps/web", "service/web", "configmap/web-config"}
	if len(got) != len(want) {
		t.Fatalf("length: got %d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
}
