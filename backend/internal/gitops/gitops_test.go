package gitops

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNoop_ReturnsDeterministicSHA(t *testing.T) {
	res, err := Noop{}.Commit(context.Background(), Request{Content: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	res2, _ := Noop{}.Commit(context.Background(), Request{Content: []byte("hello")})
	if res.CommitSHA != res2.CommitSHA {
		t.Error("Noop should be deterministic on identical input")
	}
	if len(res.CommitSHA) != 40 {
		t.Errorf("SHA length: got %d", len(res.CommitSHA))
	}
}

func TestNoop_SubstitutesImage(t *testing.T) {
	res1, _ := Noop{}.Commit(context.Background(), Request{
		Content: []byte("image: ${IMAGE}"),
		Image:   "registry/app:v1",
	})
	res2, _ := Noop{}.Commit(context.Background(), Request{
		Content: []byte("image: registry/app:v1"),
		Image:   "",
	})
	if res1.CommitSHA != res2.CommitSHA {
		t.Error("image substitution should produce the same SHA as pre-substituted content")
	}
}

func TestGoGit_RejectsMissingPath(t *testing.T) {
	// Path is required even when Repo is set; the error wraps
	// ErrUnavailable so callers can react uniformly.
	_, err := NewGoGit().Commit(context.Background(), Request{Repo: "git@example.com:x/y.git"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "Path") {
		t.Errorf("error should mention Path requirement: %v", err)
	}
}

func TestSplitAuthor(t *testing.T) {
	cases := []struct{ in, name, email string }{
		{"Alice <a@example.com>", "Alice", "a@example.com"},
		{"  Bob <b@x.io> ", "Bob", "b@x.io"},
		{"NoAngles", "NoAngles", ""},
	}
	for _, tc := range cases {
		n, e := splitAuthor(tc.in)
		if n != tc.name || e != tc.email {
			t.Errorf("splitAuthor(%q) = %q,%q want %q,%q", tc.in, n, e, tc.name, tc.email)
		}
	}
}
