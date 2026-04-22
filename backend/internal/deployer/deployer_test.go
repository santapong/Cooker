package deployer

import (
	"context"
	"errors"
	"testing"
)

func TestNoop(t *testing.T) {
	_, err := Noop{}.Deploy(context.Background(), Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
