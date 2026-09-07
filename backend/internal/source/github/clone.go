package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// CloneOptions controls public and GitHub App authenticated cloning.
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
	GitBin         string
	Commit         string
	InstallationID int64
	Token          string // in-memory only; passed via Git's process environment
}

var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func ValidCommit(s string) bool { return commitPattern.MatchString(s) }

// Clone runs `git clone` into a fresh temp directory and returns
// the absolute path of the working tree. The caller is responsible
// for removing the directory when the build is done.
func Clone(ctx context.Context, opts CloneOptions) (string, error) {
	if opts.Repo == "" {
		return "", fmt.Errorf("github: Repo is required")
	}
	// Minimal sanity check — no "../" escape, must look like owner/name.
	if !repoPattern.MatchString(opts.Repo) || strings.Contains(opts.Repo, "..") {
		return "", fmt.Errorf("github: invalid Repo %q", opts.Repo)
	}
	if opts.Commit != "" && !ValidCommit(opts.Commit) {
		return "", fmt.Errorf("github: commit must be a full SHA")
	}
	if strings.HasPrefix(opts.Branch, "-") || strings.ContainsAny(opts.Branch, "\r\n") {
		return "", fmt.Errorf("github: invalid branch")
	}
	if opts.InstallationID != 0 && opts.Token == "" {
		return "", fmt.Errorf("github: authenticated connection required")
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

	out := opts.LogWriter
	if out == nil {
		out = io.Discard
	}
	commands := [][]string{args}
	if opts.Commit != "" {
		// Fetch the reviewed object directly; moving or deleting the original
		// branch must not substitute a newer checkout or prevent a pinned build.
		commands = [][]string{{"init", dir}, {"-C", dir, "remote", "add", "origin", url}}
	}
	for _, command := range commands {
		cmd := exec.CommandContext(ctx, bin, command...)
		cmd.Env = cloneEnvironment(opts.Token)
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("git clone: %w", err)
		}
	}
	// Strip any hooks the cloned repo shipped. A malicious repo can
	// stage hooks under .git/hooks that fire on subsequent git
	// operations (commit, push, checkout) — and Cooker / its
	// downstream builders may run those operations against the
	// working tree. Clearing the directory is cheap and removes
	// the entire class of hook-driven RCE vectors. Belt-and-braces:
	// also point core.hooksPath to a non-existent path.
	hooksDir := filepath.Join(dir, ".git", "hooks")
	if err := os.RemoveAll(hooksDir); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("git clone: strip hooks: %w", err)
	}
	// Retain an empty hooks directory for tools that expect it.
	_ = os.MkdirAll(hooksDir, 0o755)
	disableHooks := exec.CommandContext(ctx, bin, "-C", dir, "config", "core.hooksPath", "/dev/null")
	disableHooks.Env = cloneEnvironment("")
	disableHooks.Stdout = io.Discard
	disableHooks.Stderr = io.Discard
	_ = disableHooks.Run()
	if opts.Commit != "" {
		for _, args := range [][]string{{"-C", dir, "fetch", "--depth=1", "origin", opts.Commit}, {"-C", dir, "checkout", "--detach", opts.Commit}} {
			cmd := exec.CommandContext(ctx, bin, args...)
			cmd.Env = cloneEnvironment(opts.Token)
			cmd.Stdout, cmd.Stderr = out, out
			if err := cmd.Run(); err != nil {
				_ = os.RemoveAll(dir)
				return "", fmt.Errorf("github: cannot checkout reviewed commit: %w", err)
			}
		}
	}
	return filepath.Clean(dir), nil
}

func cloneEnvironment(token string) []string {
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			env = append(env, v)
		}
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0=/dev/null")
	if token != "" {
		env[len(env)-3] = "GIT_CONFIG_COUNT=2"
		env = append(env, "GIT_CONFIG_KEY_1=http.https://github.com/.extraheader", "GIT_CONFIG_VALUE_1=Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)))
	}
	return env
}

func HeadCommit(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = cloneEnvironment("")
	data, err := cmd.Output()
	commit := strings.TrimSpace(string(data))
	if err != nil || !ValidCommit(commit) {
		return "", fmt.Errorf("cannot resolve repository commit")
	}
	return commit, nil
}
