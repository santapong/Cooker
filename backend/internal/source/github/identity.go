package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/santapong/cooker/internal/validate"
)

type RepositoryIdentity struct {
	ID   string
	Path string
}

type ResolvedRef struct {
	FullRef   string
	ObjectSHA string
	CommitSHA string
}

// RepositoryIdentity verifies the immutable repository ID through GitHub,
// independently of the display name supplied by a webhook.
func (c *AppClient) RepositoryIdentity(ctx context.Context, installationID int64, repo string) (RepositoryIdentity, error) {
	if c == nil {
		c = &AppClient{}
	}
	if err := validate.GitHubRepo(repo); err != nil {
		return RepositoryIdentity{}, err
	}
	token, err := c.readToken(ctx, installationID, repo)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	var result struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	}
	if err := c.request(ctx, "GET", "/repos/"+repo, token, nil, &result); err != nil {
		return RepositoryIdentity{}, err
	}
	if result.ID <= 0 || !strings.EqualFold(result.FullName, repo) {
		return RepositoryIdentity{}, fmt.Errorf("repository identity does not match")
	}
	return RepositoryIdentity{ID: strconv.FormatInt(result.ID, 10), Path: result.FullName}, nil
}

// ResolveRef keeps branches and tags distinct, and peels annotated tags to a
// commit without silently replacing a missing tag with a same-named branch.
func (c *AppClient) ResolveRef(ctx context.Context, installationID int64, repo, fullRef string) (ResolvedRef, error) {
	if c == nil {
		c = &AppClient{}
	}
	if err := validate.GitHubRepo(repo); err != nil {
		return ResolvedRef{}, err
	}
	if (!strings.HasPrefix(fullRef, "refs/heads/") && !strings.HasPrefix(fullRef, "refs/tags/")) || validate.GitRefName("ref", fullRef) != nil {
		return ResolvedRef{}, fmt.Errorf("a full branch or tag ref is required")
	}
	token, err := c.readToken(ctx, installationID, repo)
	if err != nil {
		return ResolvedRef{}, err
	}
	type object struct {
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	var result struct {
		Ref    string `json:"ref"`
		Object object `json:"object"`
	}
	if err := c.request(ctx, "GET", "/repos/"+repo+"/git/ref/"+url.PathEscape(strings.TrimPrefix(fullRef, "refs/")), token, nil, &result); err != nil {
		return ResolvedRef{}, err
	}
	if result.Ref != fullRef || !ValidCommit(result.Object.SHA) {
		return ResolvedRef{}, fmt.Errorf("source returned a different ref")
	}
	resolved := ResolvedRef{FullRef: fullRef, ObjectSHA: result.Object.SHA}
	obj := result.Object
	for depth := 0; obj.Type == "tag" && depth < 8; depth++ {
		var tag struct {
			SHA    string `json:"sha"`
			Object object `json:"object"`
		}
		if err := c.request(ctx, "GET", "/repos/"+repo+"/git/tags/"+obj.SHA, token, nil, &tag); err != nil {
			return ResolvedRef{}, err
		}
		if tag.SHA != obj.SHA || !ValidCommit(tag.Object.SHA) {
			return ResolvedRef{}, fmt.Errorf("invalid tag object")
		}
		obj = tag.Object
	}
	if obj.Type != "commit" || !ValidCommit(obj.SHA) {
		return ResolvedRef{}, fmt.Errorf("ref does not resolve to a commit")
	}
	resolved.CommitSHA = obj.SHA
	return resolved, nil
}

func (c *AppClient) readToken(ctx context.Context, id int64, repo string) (string, error) {
	if id == 0 {
		if c != nil {
			return c.AccessToken, nil
		}
		return "", nil
	}
	return c.Token(ctx, id, repo)
}
