package github

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CloneOptions controls the git clone call. Only the public GitHub
// clone URL path is supported in UAT; token-authenticated cloning
// arrives when the OAuth flow lands.
type CloneOptions struct {
	// Repo is "owner/name"; CloneURL is derived as
	// https://github.com/<Repo>.git.
	Repo string
	// Branch to check out. Empty means "default branch".
	Branch string
	// Depth clones with --depth=N. Zero means a full clone.
	Depth int
	// LogWriter, when non-nil, receives stdout+stderr of `git clone`.
	LogWriter io.Writer
	// GitBin overrides the git binary path. Empty means "git" on PATH.
	GitBin string
}

// Clone runs `git clone` into a fresh temp directory and returns
// the absolute path of the working tree. The caller is responsible
// for removing the directory when the build is done.
func Clone(ctx context.Context, opts CloneOptions) (string, error) {
	if opts.Repo == "" {
		return "", fmt.Errorf("github: Repo is required")
	}
	// Minimal sanity check — no "../" escape, must look like owner/name.
	if strings.ContainsAny(opts.Repo, " \t\n") || strings.Contains(opts.Repo, "..") {
		return "", fmt.Errorf("github: invalid Repo %q", opts.Repo)
	}
	bin := opts.GitBin
	if bin == "" {
		bin = "git"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("github: git CLI not found: %w", err)
	}

	dir, err := os.MkdirTemp("", "cooker-clone-*")
	if err != nil {
		return "", fmt.Errorf("github: tmpdir: %w", err)
	}

	url := "https://github.com/" + opts.Repo + ".git"
	args := []string{"clone"}
	if opts.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", opts.Depth))
	}
	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch)
	}
	args = append(args, url, dir)

	cmd := exec.CommandContext(ctx, bin, args...)
	out := opts.LogWriter
	if out == nil {
		out = io.Discard
	}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("git clone: %w", err)
	}
	return filepath.Clean(dir), nil
}
