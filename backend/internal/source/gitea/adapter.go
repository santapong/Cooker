package gitea

import (
	"context"
	"fmt"
	"github.com/santapong/cooker/internal/source/gittransport"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Client uses Gitea's v1 REST APIs. The connection's username/token are used only
// on its approved origin; clone_url and object URLs from the API are ignored.
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
		return nil, fmt.Errorf("gitea API and Git origins must match")
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
		h.Set("Authorization", "token "+c.credentials.Token)
	}
	return gittransport.GetJSON(ctx, c.http, c.config.APIOrigin, "/api/v1"+path, h, out)
}
func (c *Client) Repository(ctx context.Context, key string) (gittransport.Repository, error) {
	endpoint := "/repositories/" + key
	if !gittransport.ValidID(key) {
		if err := gittransport.ValidateRepositoryPath(key, false); err != nil {
			return gittransport.Repository{}, err
		}
		endpoint = "/repos/" + key
	}
	var r struct {
		ID            int64  `json:"id"`
		Path          string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := c.get(ctx, endpoint, &r); err != nil {
		return gittransport.Repository{}, err
	}
	if r.ID <= 0 || gittransport.ValidateRepositoryPath(r.Path, false) != nil {
		return gittransport.Repository{}, fmt.Errorf("invalid Gitea repository identity")
	}
	if gittransport.ValidID(key) && strconv.FormatInt(r.ID, 10) != key {
		return gittransport.Repository{}, fmt.Errorf("gitea repository identity changed")
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

type gitObject struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
}
type gitRef struct {
	Ref    string    `json:"ref"`
	Object gitObject `json:"object"`
}

func (c *Client) ResolveRevision(ctx context.Context, r gittransport.Repository, ref gittransport.Ref) (gittransport.Revision, error) {
	full, err := ref.FullName()
	if err != nil {
		return gittransport.Revision{}, err
	}
	if _, err = c.check(ctx, r); err != nil {
		return gittransport.Revision{}, err
	}
	// The refs endpoint is a prefix filter, so require one exact full-ref match.
	var rows []gitRef
	if _, err = c.get(ctx, "/repos/"+r.Path+"/git/refs/"+url.PathEscape(strings.TrimPrefix(full, "refs/")), &rows); err != nil {
		return gittransport.Revision{}, err
	}
	var object gitObject
	found := false
	for _, row := range rows {
		if row.Ref == full {
			if found {
				return gittransport.Revision{}, fmt.Errorf("ambiguous Gitea ref")
			}
			object = row.Object
			found = true
		}
	}
	if !found {
		return gittransport.Revision{}, fmt.Errorf("gitea ref not found")
	}
	if err = gittransport.ValidateCommit(object.SHA); err != nil {
		return gittransport.Revision{}, err
	}
	original := object.SHA
	seen := map[string]bool{}
	for object.Type == "tag" {
		if ref.Kind != "tag" || seen[object.SHA] || len(seen) >= 16 {
			return gittransport.Revision{}, fmt.Errorf("invalid Gitea tag chain")
		}
		seen[object.SHA] = true
		var tag struct {
			SHA    string    `json:"sha"`
			Object gitObject `json:"object"`
		}
		if _, err = c.get(ctx, "/repos/"+r.Path+"/git/tags/"+object.SHA, &tag); err != nil {
			return gittransport.Revision{}, err
		}
		if tag.SHA != object.SHA {
			return gittransport.Revision{}, fmt.Errorf("gitea returned a different tag object")
		}
		object = tag.Object
		if err = gittransport.ValidateCommit(object.SHA); err != nil {
			return gittransport.Revision{}, err
		}
	}
	if object.Type != "commit" {
		return gittransport.Revision{}, fmt.Errorf("gitea ref does not point to a commit")
	}
	result, err := gittransport.Resolved(ref, object.SHA)
	result.ObjectSHA = original
	return result, err
}
func (c *Client) VerifyCommit(ctx context.Context, r gittransport.Repository, sha string) (string, error) {
	if err := gittransport.ValidateCommit(sha); err != nil {
		return "", err
	}
	if _, err := c.check(ctx, r); err != nil {
		return "", err
	}
	var value struct {
		SHA string `json:"sha"`
	}
	if _, err := c.get(ctx, "/repos/"+r.Path+"/git/commits/"+sha, &value); err != nil {
		return "", err
	}
	if value.SHA != sha {
		return "", fmt.Errorf("gitea returned a different commit")
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
	var rows []struct {
		Name string `json:"name"`
	}
	_, err := c.get(ctx, "/repos/"+r.Path+"/"+endpoint+"?limit=50&page="+strconv.Itoa(page), &rows)
	if err != nil {
		return result, err
	}
	if len(rows) > 50 {
		return result, fmt.Errorf("gitea revision page exceeds limit")
	}
	// Resolve each typed ref to peel tags and avoid API-version-specific list SHA semantics.
	for _, row := range rows {
		rev, err := c.ResolveRevision(ctx, r, gittransport.Ref{Kind: kind, Name: row.Name})
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, rev)
	}
	if len(rows) == 50 && page < 10000 {
		result.NextPage = page + 1
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
