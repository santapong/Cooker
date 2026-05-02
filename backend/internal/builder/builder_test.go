package builder

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestNoop_ReturnsFakeDigest(t *testing.T) {
	var buf bytes.Buffer
	res, err := Noop{}.Build(context.Background(), Request{
		ContextDir: "/tmp/x",
		Tags:       []string{"app:v1"},
		LogWriter:  &buf,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ImageID == "" {
		t.Error("Noop should produce a non-empty ImageID")
	}
	if len(res.Tags) != 1 || res.Tags[0] != "app:v1" {
		t.Errorf("tags echo: got %v", res.Tags)
	}
	if buf.Len() == 0 {
		t.Error("Noop should log to the provided writer")
	}
}

func TestBuildKit_UnconfiguredReturnsUnavailable(t *testing.T) {
	_, err := (&BuildKit{}).Build(context.Background(), Request{
		ContextDir: "/tmp/x",
		Tags:       []string{"app:v1"},
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestBuildKit_RejectsMissingContext(t *testing.T) {
	_, err := NewBuildKit("tcp://buildkit:1234").Build(context.Background(), Request{
		ContextDir: "/tmp/this-path-cannot-exist-cooker-test",
		Tags:       []string{"app:v1"},
	})
	if err == nil {
		t.Fatal("expected validation error for missing ContextDir")
	}
}

func TestDockerSock_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		req  Request
	}{
		{"missing ContextDir", Request{Tags: []string{"a:1"}}},
		{"missing Tags", Request{ContextDir: "/tmp"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDockerSock().Build(context.Background(), tc.req)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDockerSock_RejectsMultiArch(t *testing.T) {
	_, err := NewDockerSock().Build(context.Background(), Request{
		ContextDir: "/tmp/x",
		Tags:       []string{"a:1"},
		Platforms:  []string{"linux/amd64", "linux/arm64"},
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable for multi-arch request, got %v", err)
	}
}
