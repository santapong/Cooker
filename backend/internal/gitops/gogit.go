package gitops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	httpauth "github.com/go-git/go-git/v5/plumbing/transport/http"
	sshauth "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// GoGit commits files using go-git. Auth resolution order:
//  1. SSHKeyPath (PEM private key on disk).
//  2. ssh-agent (when Agent==true and SSH_AUTH_SOCK is set).
//  3. HTTPS basic auth (Username/Password).
//
// Each Commit clones into a fresh temp dir and removes it after.
// Conflict-retry is intentionally minimal — anything more elaborate
// belongs in a controller layer above gitops.
type GoGit struct {
	SSHKeyPath string
	Username   string
	Password   string
	Agent      bool
}

func NewGoGit() *GoGit { return &GoGit{} }

func (g *GoGit) auth(repoURL string) (transport.AuthMethod, error) {
	if strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "http://") {
		if g.Username != "" || g.Password != "" {
			return &httpauth.BasicAuth{Username: g.Username, Password: g.Password}, nil
		}
		return nil, nil
	}
	// SSH
	if g.SSHKeyPath != "" {
		key, err := os.ReadFile(g.SSHKeyPath)
		if err != nil {
			return nil, fmt.Errorf("gogit: read key %s: %w", g.SSHKeyPath, err)
		}
		auth, err := sshauth.NewPublicKeys("git", key, "")
		if err != nil {
			return nil, fmt.Errorf("gogit: parse key: %w", err)
		}
		return auth, nil
	}
	if g.Agent {
		return sshauth.NewSSHAgentAuth("git")
	}
	return nil, nil
}

// Commit clones req.Repo, writes req.Content at req.Path on
// req.Branch, commits, and pushes back.
func (g *GoGit) Commit(ctx context.Context, req Request) (Result, error) {
	if req.Repo == "" {
		return Result{}, fmt.Errorf("%w: Repo is required", ErrUnavailable)
	}
	if req.Path == "" {
		return Result{}, fmt.Errorf("%w: Path is required", ErrUnavailable)
	}
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	auth, err := g.auth(req.Repo)
	if err != nil {
		return Result{}, err
	}
	dir, err := os.MkdirTemp("", "cooker-gitops-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)

	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           req.Repo,
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		return Result{}, fmt.Errorf("gogit: clone: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return Result{}, err
	}
	content := req.Content
	if req.Image != "" {
		content = bytes.ReplaceAll(content, []byte("${IMAGE}"), []byte(req.Image))
	}
	// Reject paths that try to escape the repo working tree.
	// filepath.Join *cleans* the result (so "a/../b" becomes "b"),
	// but it does NOT bound the result to dir — Join("/repo",
	// "../../etc/foo") returns "/etc/foo". We bound it explicitly:
	// reject absolute paths and paths whose cleaned form starts
	// with "..", and verify the joined result stays under dir.
	cleanRel := filepath.Clean(req.Path)
	if filepath.IsAbs(cleanRel) || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("gogit: path %q escapes repo", req.Path)
	}
	full := filepath.Join(dir, cleanRel)
	if !strings.HasPrefix(full, dir+string(filepath.Separator)) && full != dir {
		return Result{}, fmt.Errorf("gogit: path %q escapes repo", req.Path)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return Result{}, err
	}
	if _, err := wt.Add(cleanRel); err != nil {
		return Result{}, err
	}
	authorLine := req.Author
	if authorLine == "" {
		authorLine = "Cooker <bot@cooker>"
	}
	name, email := splitAuthor(authorLine)
	hash, err := wt.Commit(req.Message, &git.CommitOptions{
		Author: &object.Signature{Name: name, Email: email, When: time.Now()},
	})
	if err != nil {
		return Result{}, fmt.Errorf("gogit: commit: %w", err)
	}
	if err := repo.PushContext(ctx, &git.PushOptions{
		Auth:       auth,
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(plumbing.NewBranchReferenceName(branch) + ":" + plumbing.NewBranchReferenceName(branch))},
	}); err != nil {
		return Result{}, fmt.Errorf("gogit: push: %w", err)
	}
	return Result{CommitSHA: hash.String()}, nil
}

func splitAuthor(s string) (name, email string) {
	open := strings.Index(s, "<")
	closeIdx := strings.Index(s, ">")
	if open == -1 || closeIdx == -1 || open >= closeIdx {
		return strings.TrimSpace(s), ""
	}
	return strings.TrimSpace(s[:open]), strings.TrimSpace(s[open+1 : closeIdx])
}

var _ Writer = (*GoGit)(nil)
