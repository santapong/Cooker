package neon

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	apiOrigin        = "https://console.neon.tech"
	apiPrefix        = "/api/v2"
	pageSize         = 100
	maxPages         = 10
	maxItems         = 1000
	maxBodyBytes     = 1 << 20
	requestTimeout   = 15 * time.Second
	inventoryTimeout = 45 * time.Second
)

type Client struct{ http *http.Client }

func NewClient() *Client {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	}
	transport.MaxConnsPerHost, transport.MaxIdleConnsPerHost = 4, 4
	return &Client{http: &http.Client{Transport: transport, Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Projects follows the projects endpoint's pagination.cursor. Organization is
// always explicit, including when a key could otherwise infer one implicitly.
func (c *Client) Projects(ctx context.Context, key, organizationID string) (*ProjectInventory, error) {
	if !validKey(key) || !validID(organizationID) {
		return nil, &Error{Code: "invalid_input"}
	}
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	out := &ProjectInventory{OrganizationID: organizationID, Projects: []Project{}}
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; page < maxPages; page++ {
		query := url.Values{"org_id": {organizationID}, "limit": {strconv.Itoa(pageSize)}, "timeout": {"10000"}, "recoverable": {"false"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		data, err := c.get(ctx, key, "/projects", query)
		if err != nil {
			return nil, err
		}
		projects, next, err := projectPage(data, organizationID)
		if err != nil {
			return nil, err
		}
		for _, project := range projects {
			if seenIDs[project.ID] {
				return nil, &Error{Code: "incomplete_coverage"}
			}
			if len(out.Projects) >= maxItems {
				return nil, &Error{Code: "inventory_limit"}
			}
			seenIDs[project.ID] = true
			out.Projects = append(out.Projects, project)
		}
		if next == "" {
			if ctx.Err() != nil {
				return nil, &Error{Code: "canceled"}
			}
			out.Complete, out.ObservedAt = true, time.Now().UTC()
			return out, nil
		}
		if len(projects) == 0 || seenCursors[next] {
			return nil, &Error{Code: "incomplete_coverage"}
		}
		seenCursors[next], cursor = true, next
	}
	return nil, &Error{Code: "inventory_limit"}
}

// Branches uses pagination.next, not the projects cursor schema. A branch's
// name/default designation is never translated into a deployment environment.
func (c *Client) Branches(ctx context.Context, key, projectID string) (*BranchInventory, error) {
	if !validKey(key) || !validID(projectID) {
		return nil, &Error{Code: "invalid_input"}
	}
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	out := &BranchInventory{ProjectID: projectID, Branches: []Branch{}}
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; page < maxPages; page++ {
		query := url.Values{"limit": {strconv.Itoa(pageSize)}, "sort_by": {"created_at"}, "sort_order": {"asc"}, "include_deleted": {"false"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		data, err := c.get(ctx, key, "/projects/"+projectID+"/branches", query)
		if err != nil {
			return nil, err
		}
		branches, next, err := branchPage(data, projectID)
		if err != nil {
			return nil, err
		}
		for _, branch := range branches {
			if seenIDs[branch.ID] {
				return nil, &Error{Code: "incomplete_coverage"}
			}
			if len(out.Branches) >= maxItems {
				return nil, &Error{Code: "inventory_limit"}
			}
			seenIDs[branch.ID] = true
			out.Branches = append(out.Branches, branch)
		}
		if next == "" {
			if ctx.Err() != nil {
				return nil, &Error{Code: "canceled"}
			}
			out.Complete, out.ObservedAt = true, time.Now().UTC()
			return out, nil
		}
		if len(branches) == 0 || seenCursors[next] {
			return nil, &Error{Code: "incomplete_coverage"}
		}
		seenCursors[next], cursor = true, next
	}
	return nil, &Error{Code: "inventory_limit"}
}

func (c *Client) get(ctx context.Context, key, endpoint string, query url.Values) ([]byte, error) {
	if c == nil || c.http == nil {
		return nil, &Error{Code: "provider_error"}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return nil, &Error{Code: "canceled"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiOrigin+apiPrefix+endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return nil, &Error{Code: "invalid_input"}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Cooker-Neon-Metadata/1")
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &Error{Code: "canceled"}
		}
		return nil, &Error{Code: "transport"}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, responseError(response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if ctx.Err() != nil {
		return nil, &Error{Code: "canceled"}
	}
	if err != nil {
		return nil, &Error{Code: "transport"}
	}
	if len(data) > maxBodyBytes {
		return nil, &Error{Code: "response_limit"}
	}
	return data, nil
}

func responseError(response *http.Response) *Error {
	e := &Error{Code: "provider_error", StatusCode: response.StatusCode}
	switch response.StatusCode {
	case http.StatusUnauthorized:
		e.Code = "unauthorized"
	case http.StatusForbidden:
		e.Code = "forbidden"
	case http.StatusNotFound:
		e.Code = "not_found"
	case http.StatusTooManyRequests:
		e.Code = "rate_limited"
	}
	if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds >= 0 && seconds <= 3600 {
		e.RetryAfter = time.Duration(seconds) * time.Second
	}
	return e
}

func validID(value string) bool {
	if len(value) == 0 || len(value) > 60 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
			return false
		}
	}
	return true
}

func validBranchID(value string) bool {
	return validID(value) && strings.HasPrefix(value, "br-") && len(value) > 3
}

func validKey(value string) bool {
	if len(value) < 8 || len(value) > 4096 {
		return false
	}
	for _, ch := range value {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return true
}

func validCursor(value string) bool {
	if len(value) == 0 || len(value) > 2048 || !utf8.ValidString(value) {
		return false
	}
	for _, ch := range value {
		if unicode.IsControl(ch) {
			return false
		}
	}
	return true
}

func safeName(value string) (string, error) {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 256 {
		return "", &Error{Code: "invalid_response"}
	}
	for _, ch := range value {
		if unicode.IsControl(ch) {
			return "", &Error{Code: "invalid_response"}
		}
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", &Error{Code: "invalid_response"}
	}
	return value, nil
}
