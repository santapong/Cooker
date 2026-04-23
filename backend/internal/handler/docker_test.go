package handler

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveComposePath_Rejects(t *testing.T) {
	// Fix base so the test's expectations about relative-vs-absolute
	// stay stable across environments.
	prev := composeBaseDir
	t.Cleanup(func() { composeBaseDir = prev })
	composeBaseDir = t.TempDir()

	cases := []struct {
		name  string
		input string
	}{
		{"absolute path", "/etc/passwd"},
		{"relative traversal", "../etc/passwd"},
		{"nested traversal", "foo/../../etc/passwd"},
		{"subdir", "sub/compose.yml"},
		{"backslash separator", `..\etc\passwd`},
		{"hidden dotfile", ".env"},
		{"literal dot-dot", ".."},
		{"current dir", "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolveComposePath(tc.input); err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
		})
	}
}

func TestResolveComposePath_Accepts(t *testing.T) {
	base := t.TempDir()
	prev := composeBaseDir
	t.Cleanup(func() { composeBaseDir = prev })
	composeBaseDir = base

	cases := []struct {
		name  string
		input string
	}{
		{"bare filename", "docker-compose.yml"},
		{"empty defaults to compose yml", ""},
		{"alt name", "compose.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveComposePath(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(got, base+string(filepath.Separator)) {
				t.Errorf("resolved path %q must stay inside base %q", got, base)
			}
		})
	}
}
