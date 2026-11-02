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
	"strconv"
	"strings"

	"github.com/santapong/cooker/internal/source/gittransport"
)

// CloneOptions controls public and GitHub App authenticated cloning.
type CloneOptions struct {
	// Repo is "owner/name"; CloneURL is derived as
	// https://github.com/<Repo>.git.
	Repo string
	// Branch is a bare branch name or a full refs/heads/ or refs/tags/ ref.
	// Empty means the remote default branch; bare names never fall back to tags.
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
	fullRef, err := cloneRef(opts.Branch)
	if err != nil {
		return "", err
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

	// Fetch an explicit namespace before checking out its immutable commit.
	// `clone --branch name` can select a same-named tag when the branch is
	// absent, and does not accept full typed refs consistently.
	url := "https://github.com/" + opts.Repo + ".git"
	out := opts.LogWriter
	if out == nil {
		out = io.Discard
	}
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = cloneEnvironment(opts.Token)
		cmd.Stdout, cmd.Stderr = out, out
		return cmd.Run()
	}
	fail := func(err error) (string, error) {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("github: cannot checkout source: %w", err)
	}
	if err := run("init", dir); err != nil {
		return fail(err)
	}
	if err := run("-C", dir, "remote", "add", "origin", url); err != nil {
		return fail(err)
	}
	// Keep hooks disabled in the returned working tree as well as in each
	// isolated Git subprocess. No credential is written into local config.
	if err := os.RemoveAll(filepath.Join(dir, ".git", "hooks")); err != nil {
		return fail(err)
	}
	if err := run("-C", dir, "config", "core.hooksPath", "/dev/null"); err != nil {
		return fail(err)
	}
	fetchRef := fullRef
	if opts.Commit != "" {
		fetchRef = opts.Commit
	}
	args := []string{"-C", dir, "fetch", "--no-tags", "--no-recurse-submodules"}
	if opts.Commit != "" {
		args = append(args, "--depth=1")
	} else if opts.Depth > 0 {
		args = append(args, "--depth", strconv.Itoa(opts.Depth))
	}
	args = append(args, "origin", fetchRef)
	if err := run(args...); err != nil {
		return fail(err)
	}
	// ^{commit} peels annotated tags and rejects blobs/trees. FETCH_HEAD is
	// local to this fresh directory, so a ref moving later cannot alter it.
	resolve := exec.CommandContext(ctx, bin, "-C", dir, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	resolve.Env = cloneEnvironment("")
	data, err := resolve.Output()
	commit := strings.TrimSpace(string(data))
	if err != nil || gittransport.ValidateCommit(commit) != nil {
		return fail(fmt.Errorf("source ref does not resolve to a commit"))
	}
	if opts.Commit != "" && commit != opts.Commit {
		return fail(fmt.Errorf("source did not return the reviewed commit"))
	}
	if err := run("-C", dir, "checkout", "--detach", commit); err != nil {
		return fail(err)
	}
	return filepath.Clean(dir), nil
}

// cloneRef preserves branch/tag identity; only an empty input selects HEAD.
func cloneRef(value string) (string, error) {
	if value == "" {
		return "HEAD", nil
	}
	ref := gittransport.Ref{Kind: "branch", Name: value}
	if strings.HasPrefix(value, "refs/") {
		switch {
		case strings.HasPrefix(value, "refs/heads/"):
			ref.Name = strings.TrimPrefix(value, "refs/heads/")
		case strings.HasPrefix(value, "refs/tags/"):
			ref.Kind, ref.Name = "tag", strings.TrimPrefix(value, "refs/tags/")
		default:
			return "", fmt.Errorf("github: only branch or tag refs are supported")
		}
	}
	return ref.FullName()
}

func cloneEnvironment(token string) []string {
	env := gittransport.Environment("/nonexistent")
	if token != "" {
		count := 0
		for i, v := range env {
			if strings.HasPrefix(v, "GIT_CONFIG_COUNT=") {
				count, _ = strconv.Atoi(strings.TrimPrefix(v, "GIT_CONFIG_COUNT="))
				env[i] = "GIT_CONFIG_COUNT=" + strconv.Itoa(count+1)
				break
			}
		}
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=http.https://github.com/.extraheader", count), fmt.Sprintf("GIT_CONFIG_VALUE_%d=Authorization: Basic %s", count, base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))))
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
