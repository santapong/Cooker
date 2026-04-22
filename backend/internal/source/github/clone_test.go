package github

import (
	"context"
	"strings"
	"testing"
)

func TestClone_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		opts CloneOptions
	}{
		{"missing repo", CloneOptions{}},
		{"repo with space", CloneOptions{Repo: "bad owner/repo"}},
		{"repo with traversal", CloneOptions{Repo: "../../etc"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Clone(context.Background(), tc.opts)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestClone_MissingGitBin(t *testing.T) {
	_, err := Clone(context.Background(), CloneOptions{
		Repo:   "octocat/Hello-World",
		GitBin: "/nonexistent/git-binary-name-xyz",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "git CLI not found") {
		t.Errorf("unexpected error: %v", err)
	}
}
