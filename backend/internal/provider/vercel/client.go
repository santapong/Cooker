// Package vercel provides a read-only, credential-scoped Vercel inventory client.
package vercel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	apiOrigin        = "https://api.vercel.com"
	pageSize         = 100
	maxPages         = 20
	maxBodyBytes     = 4 << 20
	operationTimeout = 2 * time.Minute
	// ProjectLimit is a completeness boundary: larger inventories fail without a partial snapshot.
	ProjectLimit = 1000
	// RecentDeploymentLimit is the maximum recent history imported for each project.
	// Deployments deliberately does not promise a complete historical inventory.
	RecentDeploymentLimit = 100
)

type Identity struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
}
type Project struct {
	Details   *ProjectDetails `json:"details,omitempty"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Framework string          `json:"framework"`
}
type Deployment struct {
	Details      *DeploymentDetails `json:"details,omitempty"`
	ID           string             `json:"id"`
	ProjectID    string             `json:"projectId"`
	Name         string             `json:"name"`
	URL          string             `json:"url"`
	State        string             `json:"state"`
	Target       string             `json:"target"`
	GitBranch    string             `json:"gitBranch"`
	GitCommitSHA string             `json:"gitCommitSHA"`
	CreatedAt    time.Time          `json:"createdAt"`
}

// APIError contains only Cooker-authored messages. It never wraps provider
// response bodies or transport errors, which may contain credentials.
type APIError struct {
	StatusCode int
	RetryAfter time.Duration
	Code       string
	Message    string
}

func (e *APIError) Error() string { return e.Message }

type Client struct{ http *http.Client }

func NewClient() *Client {
	return &Client{http: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Verify binds a token to either its personal identity or the exact requested
// team ID. A team slug is deliberately not accepted as a substitute for that ID.
func (c *Client) Verify(ctx context.Context, token, teamID string) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := validateScope(token, teamID); err != nil {
		return Identity{}, err
	}
	type account struct{ ID, Name, Username, Slug string }
	var value account
	if teamID == "" {
		var response struct {
			User *account `json:"user"`
		}
		if err := c.get(ctx, token, "/v2/user", nil, &response); err != nil {
			return Identity{}, err
		}
		if response.User == nil {
			return Identity{}, invalidResponse()
		}
		value = *response.User
	} else {
		if err := c.get(ctx, token, "/v2/teams/"+teamID, nil, &value); err != nil {
			return Identity{}, err
		}
		if value.ID != teamID {
			return Identity{}, &APIError{Code: "identity_mismatch", Message: "vercel returned a different team identity"}
		}
	}
	if !validProviderID(value.ID) {
		return Identity{}, invalidResponse()
	}
	name := displayText(value.Name, 256)
	if name == "" {
		name = displayText(value.Username, 256)
	}
	if name == "" {
		name = displayText(value.Slug, 256)
	}
	if name == "" {
		name = value.ID
	}
	scope := "personal"
	if teamID != "" {
		scope = "team"
	}
	return Identity{ID: value.ID, Name: name, Scope: scope}, nil
}

type pagination struct {
	Next json.RawMessage `json:"next"`
}
type projectWire struct{ ID, Name, Framework string }

// Projects follows the current /v10/projects OpenAPI union: a bare array or an
// envelope with pagination.next (numeric timestamp or string continuation token).
// The next project cursor is sent as "from", not deployment pagination's "until".
// Source: https://openapi.vercel.sh/ (checked 2026-09-30).
func (c *Client) Projects(ctx context.Context, token, teamID string) ([]Project, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := validateScope(token, teamID); err != nil {
		return nil, err
	}
	query := scopeQuery(teamID)
	query.Set("limit", strconv.Itoa(pageSize))
	result := make([]Project, 0)
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	for page := 0; page < maxPages; page++ {
		var body json.RawMessage
		if err := c.get(ctx, token, "/v10/projects", query, &body); err != nil {
			return nil, err
		}
		var projects []projectWire
		var next string
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			if json.Unmarshal(body, &projects) != nil {
				return nil, invalidResponse()
			}
			// No cursor is documented for the bare-array variant. Never invent a
			// timestamp cursor or silently call a potentially truncated list complete.
			if len(projects) >= pageSize {
				return nil, limitError("vercel project response has no continuation cursor; inventory completeness cannot be verified")
			}
		} else {
			var response struct {
				Projects   *[]projectWire `json:"projects"`
				Pagination *pagination    `json:"pagination"`
			}
			if json.Unmarshal(body, &response) != nil || response.Projects == nil || response.Pagination == nil {
				return nil, invalidResponse()
			}
			projects = *response.Projects
			var err error
			next, err = pageCursor(response.Pagination.Next, false)
			if err != nil {
				return nil, err
			}
		}
		if len(projects) > pageSize {
			return nil, limitError("vercel project page exceeds the requested limit")
		}
		for _, p := range projects {
			if !validProviderID(p.ID) {
				return nil, invalidResponse()
			}
			if seenIDs[p.ID] {
				continue
			}
			seenIDs[p.ID] = true
			if len(result) >= ProjectLimit {
				return nil, limitError("vercel project inventory exceeds the supported limit")
			}
			result = append(result, Project{ID: p.ID, Name: displayText(p.Name, 256), Framework: displayText(p.Framework, 128)})
		}
		if next == "" {
			return result, nil
		}
		if len(projects) == 0 || seenCursors[next] {
			return nil, invalidResponse()
		}
		seenCursors[next] = true
		query.Set("from", next)
	}
	return nil, limitError("vercel project pagination exceeds the supported limit")
}

type deploymentWire struct {
	ID, UID, ProjectID, Name, URL, State, ReadyState, Target string
	Created, CreatedAt                                       int64
	Meta                                                     map[string]string
	CustomEnvironment                                        *struct{ Slug string } `json:"customEnvironment"`
}

// Deployments returns up to RecentDeploymentLimit recent entries, newest first
// in provider order. It follows pagination until that window is full or history
// ends. Older history is intentionally outside this synchronization contract.
func (c *Client) Deployments(ctx context.Context, token, teamID, projectID string) ([]Deployment, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := validateScope(token, teamID); err != nil {
		return nil, err
	}
	if !validProviderID(projectID) {
		return nil, &APIError{Code: "invalid_input", Message: "vercel project ID is invalid"}
	}
	query := scopeQuery(teamID)
	query.Set("projectId", projectID)
	result := make([]Deployment, 0)
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	for page := 0; page < maxPages; page++ {
		query.Set("limit", strconv.Itoa(RecentDeploymentLimit-len(result)))
		var response struct {
			Deployments *[]deploymentWire `json:"deployments"`
			Pagination  *pagination       `json:"pagination"`
		}
		if err := c.get(ctx, token, "/v7/deployments", query, &response); err != nil {
			return nil, err
		}
		if response.Deployments == nil || response.Pagination == nil {
			return nil, invalidResponse()
		}
		if len(*response.Deployments) > RecentDeploymentLimit-len(result) {
			return nil, limitError("vercel deployment page exceeds the requested limit")
		}
		next, err := pageCursor(response.Pagination.Next, true)
		if err != nil {
			return nil, err
		}
		for _, raw := range *response.Deployments {
			id := raw.UID
			if id == "" {
				id = raw.ID
			}
			if raw.ProjectID == "" {
				raw.ProjectID = projectID
			}
			if !validProviderID(id) || raw.ProjectID != projectID || (raw.ID != "" && raw.ID != id) {
				return nil, invalidResponse()
			}
			if seenIDs[id] {
				continue
			}
			seenIDs[id] = true
			state := raw.ReadyState
			if state == "" {
				state = raw.State
			}
			created := raw.Created
			if created == 0 {
				created = raw.CreatedAt
			}
			if created <= 0 || created > 253402300799999 {
				return nil, invalidResponse()
			}
			target := displayText(raw.Target, 128)
			if raw.CustomEnvironment != nil && raw.CustomEnvironment.Slug != "" {
				target = displayText(raw.CustomEnvironment.Slug, 128)
			}
			if target == "" {
				target = "preview"
			}
			branch, sha := gitMetadata(raw.Meta)
			result = append(result, Deployment{ID: id, ProjectID: projectID, Name: displayText(raw.Name, 256), URL: deploymentURL(raw.URL), State: deploymentState(state), Target: target, GitBranch: branch, GitCommitSHA: sha, CreatedAt: time.UnixMilli(created).UTC()})
		}
		if next == "" || len(result) == RecentDeploymentLimit {
			return result, nil
		}
		if len(*response.Deployments) == 0 || seenCursors[next] {
			return nil, invalidResponse()
		}
		seenCursors[next] = true
		query.Set("until", next)
	}
	return nil, limitError("vercel deployment pagination exceeds the supported limit")
}

func (c *Client) get(ctx context.Context, token, path string, query url.Values, out any) error {
	return c.getWithBodyLimit(ctx, token, path, query, out, maxBodyBytes)
}

// getWithBodyLimit preserves the core inventory budget while allowing optional
// inventory reads to use a smaller decoded response boundary.
func (c *Client) getWithBodyLimit(ctx context.Context, token, path string, query url.Values, out any, bodyLimit int64) error {
	u := apiOrigin + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return invalidResponse()
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Cooker-Vercel-Inventory/1")
	response, err := c.http.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return &APIError{Code: "canceled", Message: "vercel request was canceled"}
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &APIError{Code: "timeout", Message: "vercel request timed out"}
		}
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return &APIError{Code: "timeout", Message: "vercel request timed out"}
		}
		return &APIError{Code: "transport", Message: "vercel could not be reached"}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return statusError(response.StatusCode, response.Header.Get("Retry-After"), response.Header.Get("X-RateLimit-Reset"))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, bodyLimit+1))
	if err != nil {
		return &APIError{Code: "response_read", Message: "vercel response could not be read"}
	}
	if int64(len(data)) > bodyLimit {
		return limitError("vercel response exceeds the supported size")
	}
	if json.Unmarshal(data, out) != nil {
		return invalidResponse()
	}
	return nil
}

func statusError(status int, retry, reset string) *APIError {
	now := time.Now()
	e := &APIError{StatusCode: status, Code: "provider_error", Message: "vercel rejected the inventory request", RetryAfter: retryDelay(retry, now)}
	switch status {
	case 401:
		e.Code, e.Message = "unauthorized", "vercel token is invalid or expired"
	case 403:
		e.Code, e.Message = "forbidden", "vercel token cannot access the selected account or resource"
	case 404:
		e.Code, e.Message = "not_found", "vercel account or resource was not found"
	case 429:
		e.Code, e.Message = "rate_limited", "vercel rate limit reached; retry later"
		// Vercel documents X-RateLimit-Reset as UTC epoch seconds. Honor it
		// only for rate-limit responses, bounded like Retry-After.
		if delay := resetDelay(reset, now); delay > e.RetryAfter {
			e.RetryAfter = delay
		}
	default:
		if status >= 300 && status < 400 {
			e.Code, e.Message = "redirect_blocked", "vercel redirect was refused"
		}
		if status >= 500 {
			e.Code, e.Message = "unavailable", "vercel is temporarily unavailable"
		}
	}
	return e
}

func resetDelay(value string, now time.Time) time.Duration {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds <= now.Unix() {
		return 0
	}
	if seconds > now.Unix()+24*60*60 {
		return 24 * time.Hour
	}
	return time.Unix(seconds, 0).Sub(now)
}

func retryDelay(value string, now time.Time) time.Duration {
	const maximum = 24 * time.Hour
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds >= int64(maximum/time.Second) {
			return maximum
		}
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		delay := date.Sub(now)
		if delay > maximum {
			return maximum
		}
		if delay > 0 {
			return delay
		}
	}
	return 0
}

func validateScope(token, teamID string) error {
	if len(token) == 0 || len(token) > 8192 {
		return &APIError{Code: "invalid_input", Message: "vercel token is invalid"}
	}
	for _, ch := range token {
		if ch < 33 || ch > 126 {
			return &APIError{Code: "invalid_input", Message: "vercel token is invalid"}
		}
	}
	if teamID != "" && (!validProviderID(teamID) || !strings.HasPrefix(teamID, "team_")) {
		return &APIError{Code: "invalid_input", Message: "vercel team ID is invalid"}
	}
	return nil
}

func validProviderID(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_', ch == '-':
		default:
			return false
		}
	}
	return true
}

func scopeQuery(teamID string) url.Values {
	query := url.Values{}
	if teamID != "" {
		query.Set("teamId", teamID)
	}
	return query
}

func pageCursor(raw json.RawMessage, timestampOnly bool) (string, error) {
	if len(raw) == 0 {
		return "", invalidResponse()
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	value := string(raw)
	if raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return "", invalidResponse()
		}
	}
	if len(value) == 0 || len(value) > 4096 {
		return "", invalidResponse()
	}
	if timestampOnly || raw[0] != '"' {
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil || number <= 0 {
			return "", invalidResponse()
		}
	} else {
		for _, ch := range value {
			if ch < 33 || ch > 126 {
				return "", invalidResponse()
			}
		}
	}
	return value, nil
}

func invalidResponse() *APIError {
	return &APIError{Code: "invalid_response", Message: "vercel returned an invalid inventory response"}
}
func limitError(message string) *APIError {
	return &APIError{Code: "inventory_limit", Message: message}
}

func displayText(value string, max int) string {
	if len(value) > max {
		return ""
	}
	for _, ch := range value {
		if unicode.IsControl(ch) || unicode.In(ch, unicode.Cf) {
			return ""
		}
	}
	return strings.TrimSpace(value)
}

func deploymentState(value string) string {
	switch strings.ToUpper(value) {
	case "READY", "BUILDING", "QUEUED", "INITIALIZING", "CANCELED", "ERROR", "BLOCKED", "DELETED":
		return strings.ToUpper(value)
	default:
		return "UNKNOWN"
	}
}

func gitMetadata(meta map[string]string) (string, string) {
	for _, prefix := range []string{"githubCommit", "gitlabCommit", "bitbucketCommit", "gitCommit"} {
		branch := displayText(meta[prefix+"Ref"], 512)
		sha := strings.ToLower(meta[prefix+"Sha"])
		if branch == "" && sha == "" {
			continue
		}
		if len(sha) != 40 && len(sha) != 64 {
			sha = ""
		}
		for _, ch := range sha {
			if (ch < 'a' || ch > 'f') && (ch < '0' || ch > '9') {
				sha = ""
				break
			}
		}
		return branch, sha
	}
	return "", ""
}

// Deployment URLs are display links, never fetch targets. Accept provider host
// names or HTTPS URLs, but exclude credentials, ports, IPs, local names, escaped
// authorities, query strings, and fragments. Unusable links are omitted.
func deploymentURL(value string) string {
	if value == "" || len(value) > 2048 || displayText(value, 2048) != value || strings.ContainsAny(value, "\\%") {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Host == "" || u.Port() != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) != nil || !strings.Contains(host, ".") || len(host) > 253 {
		return ""
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".test", ".invalid", ".example", ".onion"} {
		if strings.HasSuffix(host, suffix) {
			return ""
		}
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, ch := range label {
			switch {
			case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-':
			default:
				return ""
			}
		}
	}
	// Numeric pseudo-hosts can be interpreted as IPv4 by browsers.
	top := host[strings.LastIndex(host, ".")+1:]
	if _, err := strconv.ParseUint(top, 10, 64); err == nil {
		return ""
	}
	if _, err := strconv.ParseUint(top, 0, 64); err == nil {
		return ""
	}
	u.Host = host
	return u.String()
}
