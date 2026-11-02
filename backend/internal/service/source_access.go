package service

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/source/gitea"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/source/gitlab"
	"github.com/santapong/cooker/internal/source/gittransport"
)

type SourceAccess struct {
	Connections *connection.Service
	GitHub      *github.AppClient
}

func (s *SourceAccess) adapter(c *connection.Connection) (gittransport.Adapter, error) {
	credentials, err := s.Connections.OpenCredentials(c)
	if err != nil {
		return nil, err
	}
	creds := gittransport.Credentials{Username: c.Username, Token: credentials.Token}
	switch c.Provider {
	case "gitlab":
		return gitlab.NewClient(c.TransportConfig(), creds)
	case "gitea":
		return gitea.NewClient(c.TransportConfig(), creds)
	}
	return nil, fmt.Errorf("source provider is unavailable")
}
func (s *SourceAccess) github(c *connection.Connection) (*github.AppClient, error) {
	client := &github.AppClient{}
	if s.GitHub != nil {
		cp := *s.GitHub
		client = &cp
	}
	cred, err := s.Connections.OpenCredentials(c)
	if err != nil {
		return nil, err
	}
	client.AccessToken = cred.Token
	return client, nil
}
func (s *SourceAccess) Identity(ctx context.Context, c *connection.Connection, repo string) (model.GitSourceIdentity, error) {
	if c == nil || !c.Allows(repo) {
		return model.GitSourceIdentity{}, fmt.Errorf("source repository is not allowed")
	}
	u, err := url.Parse(c.Origin)
	if err != nil {
		return model.GitSourceIdentity{}, err
	}
	id := model.GitSourceIdentity{Provider: c.Provider, ConnectionID: c.ID, Host: u.Host, Repository: repo}
	if c.Provider == "github" {
		client, err := s.github(c)
		if err != nil {
			return id, err
		}
		r, err := client.RepositoryIdentity(ctx, c.InstallationID, repo)
		if err != nil {
			return id, err
		}
		id.RepositoryID = r.ID
		id.Repository = r.Path
		return id, nil
	}
	adapter, err := s.adapter(c)
	if err != nil {
		return id, err
	}
	r, err := adapter.Repository(ctx, repo)
	if err != nil {
		return id, err
	}
	if !c.Allows(r.Path) {
		return id, fmt.Errorf("resolved repository is outside connection allowlist")
	}
	id.RepositoryID = r.ID
	id.Repository = r.Path
	return id, nil
}
func (s *SourceAccess) Revision(ctx context.Context, c *connection.Connection, repo, fullRef string) (SourceRevision, error) {
	if !c.Allows(repo) {
		return SourceRevision{}, fmt.Errorf("source repository is not allowed")
	}
	if c.Provider == "github" {
		client, err := s.github(c)
		if err != nil {
			return SourceRevision{}, err
		}
		r, err := client.ResolveRef(ctx, c.InstallationID, repo, fullRef)
		return SourceRevision{FullRef: r.FullRef, Commit: r.CommitSHA, Object: r.ObjectSHA}, err
	}
	ref, err := parseSourceRef(fullRef)
	if err != nil {
		return SourceRevision{}, err
	}
	adapter, err := s.adapter(c)
	if err != nil {
		return SourceRevision{}, err
	}
	r, err := adapter.Repository(ctx, repo)
	if err != nil {
		return SourceRevision{}, err
	}
	rev, err := adapter.ResolveRevision(ctx, r, ref)
	return SourceRevision{FullRef: rev.FullRef, Commit: rev.Commit, Object: rev.ObjectSHA}, err
}
func parseSourceRef(full string) (gittransport.Ref, error) {
	for _, kind := range []string{"branch", "tag"} {
		prefix := "refs/heads/"
		if kind == "tag" {
			prefix = "refs/tags/"
		}
		if strings.HasPrefix(full, prefix) {
			r := gittransport.Ref{Kind: kind, Name: strings.TrimPrefix(full, prefix)}
			_, err := r.FullName()
			return r, err
		}
	}
	return gittransport.Ref{}, fmt.Errorf("full source ref is required")
}
func (s *SourceAccess) appConnection(ctx context.Context, app *model.App) (*connection.Connection, error) {
	if app == nil || app.BuildPlan == nil || app.BuildPlan.SourceConnectionID == "" {
		return nil, fmt.Errorf("source connection is required")
	}
	c, err := s.Connections.Store.Get(ctx, app.BuildPlan.SourceConnectionID)
	if err != nil {
		return nil, err
	}
	if !c.Allows(app.GitHubRepo) || app.BuildPlan.SourceConnectionVersion != c.Version {
		return nil, fmt.Errorf("source connection changed; inspect and save its current version")
	}
	id, err := s.Identity(ctx, c, app.GitHubRepo)
	if err != nil {
		return nil, err
	}
	if app.BuildPlan.SourceRepositoryID == "" || id.RepositoryID != app.BuildPlan.SourceRepositoryID {
		return nil, fmt.Errorf("source repository identity changed")
	}
	return c, nil
}
func (s *SourceAccess) ResolveApp(ctx context.Context, app *model.App, ref string) (string, error) {
	if app == nil {
		return "", fmt.Errorf("app is required")
	}
	if !github.ValidCommit(ref) {
		if !strings.HasPrefix(ref, "refs/") {
			ref = fullTriggerRef("branch", ref)
		}
		if _, err := parseSourceRef(ref); err != nil {
			return "", err
		}
	}
	if app.BuildPlan == nil || app.BuildPlan.SourceConnectionID == "" {
		var installation int64
		if app.BuildPlan != nil {
			installation = app.BuildPlan.InstallationID
		}
		if github.ValidCommit(ref) {
			return s.GitHub.ResolveCommit(ctx, installation, app.GitHubRepo, ref)
		}
		r, err := s.GitHub.ResolveRef(ctx, installation, app.GitHubRepo, ref)
		return r.CommitSHA, err
	}
	c, err := s.appConnection(ctx, app)
	if err != nil {
		return "", err
	}
	if github.ValidCommit(ref) {
		if c.Provider == "github" {
			client, err := s.github(c)
			if err != nil {
				return "", err
			}
			return client.ResolveCommit(ctx, c.InstallationID, app.GitHubRepo, ref)
		}
		adapter, err := s.adapter(c)
		if err != nil {
			return "", err
		}
		r, err := adapter.Repository(ctx, app.GitHubRepo)
		if err != nil {
			return "", err
		}
		return adapter.VerifyCommit(ctx, r, ref)
	}
	r, err := s.Revision(ctx, c, app.GitHubRepo, ref)
	return r.Commit, err
}
func (s *SourceAccess) Checkout(ctx context.Context, app *model.App, commit string, writer io.Writer) (string, error) {
	c, err := s.appConnection(ctx, app)
	if err != nil {
		return "", err
	}
	if commit == "" {
		commit, err = s.ResolveApp(ctx, app, app.Branch)
		if err != nil {
			return "", err
		}
	}
	return s.checkoutConnection(ctx, c, app.GitHubRepo, app.BuildPlan.SourceRepositoryID, commit, writer)
}

// CheckoutSource rechecks exact connection generation and immutable provider
// identity before opening credentials. No synthetic App grants native authority.
func (s *SourceAccess) CheckoutSource(ctx context.Context, plan *model.PipelineSourcePlan, writer io.Writer) (string, error) {
	if s == nil || s.Connections == nil || s.Connections.Store == nil || plan == nil || gittransport.ValidateCommit(plan.CommitSHA) != nil {
		return "", fmt.Errorf("accepted native source is required")
	}
	c, err := s.Connections.Store.Get(ctx, plan.Source.ConnectionID)
	if err != nil || !c.Allows(plan.Source.Repository) || c.Version != plan.ConnectionVersion {
		return "", fmt.Errorf("source connection changed after admission")
	}
	id, err := s.Identity(ctx, c, plan.Source.Repository)
	if err != nil || !id.SameRepository(plan.Source) || id.Repository != plan.Source.Repository {
		return "", fmt.Errorf("source repository identity changed after admission")
	}
	return s.checkoutConnection(ctx, c, id.Repository, id.RepositoryID, plan.CommitSHA, writer)
}

func (s *SourceAccess) checkoutConnection(ctx context.Context, c *connection.Connection, repository, repositoryID, commit string, writer io.Writer) (string, error) {
	if c.Provider == "github" {
		cred, err := s.Connections.OpenCredentials(c)
		if err != nil {
			return "", err
		}
		if c.AuthMode == "github_app" {
			client, err := s.github(c)
			if err != nil {
				return "", err
			}
			cred.Token, err = client.Token(ctx, c.InstallationID, repository)
			if err != nil {
				return "", err
			}
		}
		username := c.Username
		if cred.Token != "" && username == "" {
			username = "x-access-token"
		}
		return gittransport.CloneExact(ctx, gittransport.CloneOptions{Config: c.TransportConfig(), Credentials: gittransport.Credentials{Username: username, Token: cred.Token}, Repository: repository, Commit: commit, LogWriter: writer})
	}
	adapter, err := s.adapter(c)
	if err != nil {
		return "", err
	}
	r, err := adapter.Repository(ctx, repository)
	if err != nil {
		return "", err
	}
	if r.ID != repositoryID {
		return "", fmt.Errorf("source repository identity changed")
	}
	return adapter.Checkout(ctx, r, commit, writer)
}
