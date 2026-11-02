package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Real Git exercises namespace selection and annotated tag peeling. The test
// wrapper replaces only the fixed GitHub origin with this disposable fixture;
// allowing file transport here does not alter the production environment.
func TestCloneTypedRefsUseExactCommit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "remote")
	command := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Env = cloneEnvironment("")
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git %v: %s: %v", args, data, err)
		}
		return strings.TrimSpace(string(data))
	}
	command("init", "--initial-branch=release/one", repo)
	commit := func(contents string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		command("-C", repo, "add", "source.txt")
		command("-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", contents)
		return command("-C", repo, "rev-parse", "HEAD")
	}
	tagCommit := commit("tag contents")
	command("-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "tag", "-a", "release/one", "-m", "annotated release")
	command("-C", repo, "tag", "tag-only")
	branchCommit := commit("branch contents")
	wrapper := filepath.Join(root, "git-fixture")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$3" = remote ] && [ "$4" = add ] && [ "$5" = origin ]; then
  exec %q -C "$2" remote add origin %q
fi
exec %q -c protocol.file.allow=always "$@"
`, git, repo, git)
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ref, want string
	}{
		{"release/one", branchCommit},
		{"refs/heads/release/one", branchCommit},
		{"refs/tags/release/one", tagCommit},
		{"refs/tags/tag-only", tagCommit},
		{"", branchCommit},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			dir, err := Clone(context.Background(), CloneOptions{Repo: "acme/shop", Branch: tc.ref, GitBin: wrapper, Depth: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			head, err := HeadCommit(context.Background(), dir)
			if err != nil || head != tc.want {
				t.Fatalf("ref %q selected %s, want %s: %v", tc.ref, head, tc.want, err)
			}
			if symbolic := command("-C", dir, "rev-parse", "--abbrev-ref", "HEAD"); symbolic != "HEAD" {
				t.Fatal("checkout is not detached at an immutable commit")
			}
		})
	}
	for _, ref := range []string{"tag-only", "refs/heads/tag-only", "refs/remotes/origin/release/one", "refs/tags/", "main~1"} {
		t.Run("reject "+ref, func(t *testing.T) {
			dir, err := Clone(context.Background(), CloneOptions{Repo: "acme/shop", Branch: ref, GitBin: wrapper, Depth: 1})
			if dir != "" {
				defer os.RemoveAll(dir)
			}
			if err == nil {
				t.Fatalf("invalid or missing branch %q fell back to a tag", ref)
			}
		})
	}
	// A reviewed commit must win even after its original ref has moved.
	dir, err := Clone(context.Background(), CloneOptions{Repo: "acme/shop", Branch: "release/one", Commit: tagCommit, GitBin: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if head, err := HeadCommit(context.Background(), dir); err != nil || head != tagCommit {
		t.Fatalf("reviewed commit changed: %s %v", head, err)
	}
}
