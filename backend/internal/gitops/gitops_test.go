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

func TestGoGit_Unwired(t *testing.T) {
	_, err := NewGoGit().Commit(context.Background(), Request{Repo: "git@example.com:x/y.git"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "go-git") {
		t.Errorf("error should mention go-git: %v", err)
	}
}
