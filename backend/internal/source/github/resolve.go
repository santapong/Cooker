package github

import (
	"context"
	"fmt"
	"net/url"

	"github.com/santapong/cooker/internal/validate"
)

// ResolveCommit resolves a branch, tag or full SHA before accepting a deploy.
// The worker still verifies the checkout against the resulting immutable SHA.
func (c *AppClient) ResolveCommit(ctx context.Context, installationID int64, repo, ref string) (string, error) {
	if err := validate.GitHubRepo(repo); err != nil {
		return "", err
	}
	if err := validate.GitRefName("source ref", ref); err != nil {
		return "", err
	}
	if ref == "" {
		return "", fmt.Errorf("source ref is required")
	}
	if c == nil {
		c = &AppClient{}
	}
	token := c.AccessToken
	if installationID != 0 {
		var err error
		token, err = c.Token(ctx, installationID, repo)
		if err != nil {
			return "", err
		}
	}
	var result struct {
		SHA string `json:"sha"`
	}
	if err := c.request(ctx, "GET", "/repos/"+repo+"/commits/"+url.PathEscape(ref), token, nil, &result); err != nil {
		return "", err
	}
	if !ValidCommit(result.SHA) {
		return "", fmt.Errorf("source provider did not return a full commit SHA")
	}
	return result.SHA, nil
}
