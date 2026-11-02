package gitlab

import (
	"context"
	"fmt"
	"github.com/santapong/cooker/internal/source/gittransport"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

// Client supports GitLab v4 REST APIs and exact HTTPS checkout. Credentials are
// connection-scoped personal/project/group access tokens (not CI/deploy tokens).
type Client struct {
	config      gittransport.Config
	credentials gittransport.Credentials
	http        *http.Client
}

var _ gittransport.Adapter = (*Client)(nil)

func NewClient(config gittransport.Config, credentials gittransport.Credentials) (*Client, error) {
	if config.APIOrigin == "" {
		config.APIOrigin = config.Origin
	}
	if config.APIOrigin != config.Origin {
		return nil, fmt.Errorf("GitLab API and Git origins must match")
	}
	if credentials.Token != "" && credentials.Username == "" {
		credentials.Username = "oauth2"
	}
	if err := gittransport.ValidateCredentials(credentials); err != nil {
		return nil, err
	}
	client, err := gittransport.NewHTTPClient(config)
	if err != nil {
		return nil, err
	}
	return &Client{config: config, credentials: credentials, http: client}, nil
}
func (c *Client) get(ctx context.Context, path string, out any) (http.Header, error) {
	h := http.Header{}
	if c.credentials.Token != "" {
		h.Set("PRIVATE-TOKEN", c.credentials.Token)
	}
	return gittransport.GetJSON(ctx, c.http, c.config.APIOrigin, "/api/v4"+path, h, out)
}
func (c *Client) Repository(ctx context.Context, key string) (gittransport.Repository, error) {
	if !gittransport.ValidID(key) {
		if err := gittransport.ValidateRepositoryPath(key, true); err != nil {
			return gittransport.Repository{}, err
		}
	}
	var r struct {
		ID            int64  `json:"id"`
		Path          string `json:"path_with_namespace"`
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := c.get(ctx, "/projects/"+url.PathEscape(key), &r); err != nil {
		return gittransport.Repository{}, err
	}
	if r.ID <= 0 || gittransport.ValidateRepositoryPath(r.Path, true) != nil {
		return gittransport.Repository{}, fmt.Errorf("invalid GitLab repository identity")
	}
	if gittransport.ValidID(key) && strconv.FormatInt(r.ID, 10) != key {
		return gittransport.Repository{}, fmt.Errorf("GitLab repository identity changed")
	}
	return gittransport.Repository{ID: strconv.FormatInt(r.ID, 10), Path: r.Path, Origin: c.config.Origin, DefaultBranch: r.DefaultBranch}, nil
}
func (c *Client) check(ctx context.Context, r gittransport.Repository) (gittransport.Repository, error) {
	if r.Origin != c.config.Origin || !gittransport.ValidID(r.ID) {
		return gittransport.Repository{}, fmt.Errorf("repository does not belong to this source connection")
	}
	current, err := c.Repository(ctx, r.ID)
	if err != nil {
		return current, err
	}
	if current.Path != r.Path {
		return current, fmt.Errorf("repository path changed; refresh source identity before continuing")
	}
	return current, nil
}

type gitlabRevision struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
}

func (c *Client) ResolveRevision(ctx context.Context, r gittransport.Repository, ref gittransport.Ref) (gittransport.Revision, error) {
	if _, err := ref.FullName(); err != nil {
		return gittransport.Revision{}, err
	}
	if _, err := c.check(ctx, r); err != nil {
		return gittransport.Revision{}, err
	}
	kind := "branches"
	if ref.Kind == "tag" {
		kind = "tags"
	}
	var value gitlabRevision
	if _, err := c.get(ctx, "/projects/"+r.ID+"/repository/"+kind+"/"+url.PathEscape(ref.Name), &value); err != nil {
		return gittransport.Revision{}, err
	}
	if value.Name != ref.Name {
		return gittransport.Revision{}, fmt.Errorf("GitLab returned a different ref")
	}
	result, err := gittransport.Resolved(ref, value.Commit.ID)
	if err != nil {
		return result, err
	}
	result.ObjectSHA = result.Commit
	if ref.Kind == "tag" {
		if err := gittransport.ValidateCommit(value.Target); err != nil {
			return gittransport.Revision{}, fmt.Errorf("invalid GitLab tag object")
		}
		result.ObjectSHA = value.Target
	}
	return result, nil
}
func (c *Client) VerifyCommit(ctx context.Context, r gittransport.Repository, sha string) (string, error) {
	if err := gittransport.ValidateCommit(sha); err != nil {
		return "", err
	}
	if _, err := c.check(ctx, r); err != nil {
		return "", err
	}
	var value struct {
		ID string `json:"id"`
	}
	if _, err := c.get(ctx, "/projects/"+r.ID+"/repository/commits/"+sha, &value); err != nil {
		return "", err
	}
	if value.ID != sha {
		return "", fmt.Errorf("GitLab returned a different commit")
	}
	return sha, nil
}
func (c *Client) ListRevisions(ctx context.Context, r gittransport.Repository, kind string, page int) (gittransport.RevisionPage, error) {
	result := gittransport.RevisionPage{Items: []gittransport.Revision{}}
	if page < 1 || page > 10000 {
		return result, fmt.Errorf("invalid source page")
	}
	if _, err := (gittransport.Ref{Kind: kind, Name: "probe"}).FullName(); err != nil {
		return result, err
	}
	if _, err := c.check(ctx, r); err != nil {
		return result, err
	}
	endpoint := "branches"
	if kind == "tag" {
		endpoint = "tags"
	}
	var rows []gitlabRevision
	headers, err := c.get(ctx, "/projects/"+r.ID+"/repository/"+endpoint+"?per_page=50&page="+strconv.Itoa(page), &rows)
	if err != nil {
		return result, err
	}
	if len(rows) > 50 {
		return result, fmt.Errorf("GitLab revision page exceeds limit")
	}
	for _, row := range rows {
		rev, err := gittransport.Resolved(gittransport.Ref{Kind: kind, Name: row.Name}, row.Commit.ID)
		if err != nil {
			return result, err
		}
		rev.ObjectSHA = rev.Commit
		if kind == "tag" {
			if err := gittransport.ValidateCommit(row.Target); err != nil {
				return result, err
			}
			rev.ObjectSHA = row.Target
		}
		result.Items = append(result.Items, rev)
	}
	if n := headers.Get("X-Next-Page"); n != "" {
		next, e := strconv.Atoi(n)
		if e != nil || next != page+1 || next > 10000 {
			return result, fmt.Errorf("invalid GitLab pagination")
		}
		result.NextPage = next
	}
	return result, nil
}
func (c *Client) Checkout(ctx context.Context, r gittransport.Repository, sha string, log io.Writer) (string, error) {
	if _, err := c.VerifyCommit(ctx, r, sha); err != nil {
		return "", err
	}
	dir, err := gittransport.CloneExact(ctx, gittransport.CloneOptions{Config: c.config, Credentials: c.credentials, Repository: r.Path, Commit: sha, LogWriter: log})
	if err != nil {
		return "", err
	}
	if _, err = c.check(ctx, r); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}
