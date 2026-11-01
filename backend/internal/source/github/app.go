package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppClient is an operator-managed, read-only GitHub App connection. Only the
// installations explicitly allowed by the server administrator can be used.
// Tokens are short lived, scoped to repository contents, and never sent to UI.
type AppClient struct {
	AppID           string
	Slug            string
	Key             *rsa.PrivateKey
	InstallationIDs []int64
	HTTP            *http.Client
	BaseURL         string
}

type Installation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
	} `json:"account"`
}
type Repository struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}
type Revision struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

func NewAppClient(appID, slug, keyFile string, installations []string) (*AppClient, error) {
	c := &AppClient{AppID: appID, Slug: slug}
	if appID == "" && keyFile == "" {
		return c, nil
	}
	if appID == "" || keyFile == "" {
		return nil, fmt.Errorf("GitHub App ID and private key file are both required")
	}
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read GitHub App private key file")
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("GitHub App private key must be PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		c.Key = key
	} else {
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid GitHub App private key")
		}
		c.Key, _ = key.(*rsa.PrivateKey)
	}
	if c.Key == nil {
		return nil, fmt.Errorf("GitHub App requires an RSA private key")
	}
	for _, value := range installations {
		id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid GitHub installation ID")
		}
		c.InstallationIDs = append(c.InstallationIDs, id)
	}
	return c, nil
}

func (c *AppClient) Configured() bool { return c != nil && c.Key != nil && c.AppID != "" }
func (c *AppClient) InstallURL() string {
	if c == nil || c.Slug == "" {
		return ""
	}
	return "https://github.com/apps/" + url.PathEscape(c.Slug) + "/installations/new"
}
func (c *AppClient) allowed(id int64) bool {
	if !c.Configured() {
		return false
	}
	for _, allowed := range c.InstallationIDs {
		if id == allowed {
			return true
		}
	}
	return false
}
func (c *AppClient) jwt() (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("GitHub App connection is not configured")
	}
	now := time.Now()
	return jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer: c.AppID, IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)), ExpiresAt: jwt.NewNumericDate(now.Add(8 * time.Minute)),
	}).SignedString(c.Key)
}

func (c *AppClient) request(ctx context.Context, method, path, token string, body, out any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid GitHub request")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Cooker")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub request failed; check connectivity")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("GitHub returned HTTP %d; check repository access, installation permissions, or rate limits", res.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("invalid GitHub response")
	}
	return nil
}

func (c *AppClient) Installations(ctx context.Context) ([]Installation, error) {
	items := []Installation{}
	token, err := c.jwt()
	if err != nil {
		return items, err
	}
	for _, id := range c.InstallationIDs {
		var item Installation
		if err := c.request(ctx, "GET", fmt.Sprintf("/app/installations/%d", id), token, nil, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (c *AppClient) Token(ctx context.Context, id int64, repo string) (string, error) {
	if !c.allowed(id) {
		return "", fmt.Errorf("GitHub installation is not connected to this Cooker workspace")
	}
	token, err := c.jwt()
	if err != nil {
		return "", err
	}
	body := map[string]any{"permissions": map[string]string{"contents": "read"}}
	if repo != "" {
		if !repoPattern.MatchString(repo) || strings.Contains(repo, "..") {
			return "", fmt.Errorf("invalid repository")
		}
		parts := strings.Split(repo, "/")
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid repository")
		}
		// Verify the installation owns this full repository, not just its basename.
		var installation Installation
		if err := c.request(ctx, "GET", "/repos/"+repo+"/installation", token, nil, &installation); err != nil {
			return "", err
		}
		if installation.ID != id {
			return "", fmt.Errorf("repository belongs to a different GitHub installation")
		}
		body["repositories"] = []string{parts[1]}
	}
	var res struct {
		Token string `json:"token"`
	}
	if err := c.request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", id), token, body, &res); err != nil {
		return "", err
	}
	if res.Token == "" {
		return "", fmt.Errorf("GitHub returned an empty installation token")
	}
	return res.Token, nil
}

func (c *AppClient) Repositories(ctx context.Context, id int64, page int) ([]Repository, error) {
	token, err := c.Token(ctx, id, "")
	if err != nil {
		return nil, err
	}
	var res struct {
		Repositories []Repository `json:"repositories"`
	}
	if err := c.request(ctx, "GET", fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page), token, nil, &res); err != nil {
		return nil, err
	}
	if res.Repositories == nil {
		res.Repositories = []Repository{}
	}
	return res.Repositories, nil
}

func (c *AppClient) Revisions(ctx context.Context, id int64, repo, kind string, page int) ([]Revision, error) {
	token := ""
	if id != 0 {
		var err error
		token, err = c.Token(ctx, id, repo)
		if err != nil {
			return nil, err
		}
	}
	if kind != "tags" {
		kind = "branches"
	}
	items := []Revision{}
	if err := c.request(ctx, "GET", fmt.Sprintf("/repos/%s/%s?per_page=100&page=%d", repo, kind, page), token, nil, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *AppClient) Clone(ctx context.Context, opts CloneOptions) (string, error) {
	if opts.InstallationID != 0 {
		var err error
		opts.Token, err = c.Token(ctx, opts.InstallationID, opts.Repo)
		if err != nil {
			return "", err
		}
	}
	return Clone(ctx, opts)
}
