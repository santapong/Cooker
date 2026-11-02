package vercel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// ErrMutationUnknown means a publishing request may have taken effect. The
// caller must retain any returned ID and reconcile; it must not repeat the POST.
var ErrMutationUnknown = errors.New("vercel mutation outcome is unknown; reconcile before retrying")

const (
	MaxStaticFiles      = 100
	MaxStaticFileBytes  = 4 << 20
	MaxStaticTotalBytes = 8 << 20
)

// StaticAPI is deliberately separate from inventory access. The caller must
// authorize publishing and durably record its intent before either Create call.
type StaticAPI interface {
	CreateStaticProject(context.Context, string, string, string) (Project, error)
	CreateStaticDeployment(context.Context, string, string, StaticDeploymentRequest) (StaticDeploymentResult, error)
	GetStaticDeployment(context.Context, string, string, string) (StaticDeploymentResult, error)
}

type StaticFile struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
}

type StaticDeploymentRequest struct {
	ProjectID      string       `json:"projectId"`
	Name           string       `json:"name"`
	OperationID    string       `json:"operationId"`
	ArtifactSHA256 string       `json:"artifactSha256"`
	Files          []StaticFile `json:"files"`
}

// StaticDeploymentResult may contain only ID and State for a reduced acknowledgement. Optional
// fields are never synthesized from the request as proof of provider identity.
// Reconciliation must verify project, owner and Cooker metadata from a later GET.
type StaticDeploymentResult struct {
	ID             string `json:"id"`
	ProjectID      string `json:"projectId,omitempty"`
	OwnerID        string `json:"ownerId,omitempty"`
	OperationID    string `json:"operationId,omitempty"`
	ArtifactSHA256 string `json:"artifactSha256,omitempty"`
	State          string `json:"state,omitempty"`
	URL            string `json:"url,omitempty"`
	Target         string `json:"target,omitempty"`
}

// CreateStaticProject always creates a dedicated project. A name conflict is a
// rejection, never permission to adopt or alter an existing project.
// Vercel's v11 schema supports these settings; Other + empty build/install
// commands serves static files without running user build code.
// https://vercel.com/docs/builds/configure-a-build#skip-build-step
func (c *Client) CreateStaticProject(ctx context.Context, token, teamID, name string) (Project, error) {
	if err := validateScope(token, teamID); err != nil {
		return Project{}, err
	}
	if !staticProjectName(name) {
		return Project{}, staticInvalid("vercel static project name is invalid")
	}
	body, err := json.Marshal(struct {
		Name            string  `json:"name"`
		Framework       *string `json:"framework"`
		BuildCommand    string  `json:"buildCommand"`
		InstallCommand  string  `json:"installCommand"`
		OutputDirectory string  `json:"outputDirectory"`
	}{Name: name, OutputDirectory: "."})
	if err != nil {
		return Project{}, staticInvalid("vercel project request could not be encoded")
	}
	data, requestErr := c.staticRequest(ctx, token, http.MethodPost, "/v11/projects", scopeQuery(teamID), body)
	fields, parseErr := staticObject(data)
	var wire struct{ ID, Name, Framework, AccountID string }
	decodeErr := staticDecode(fields, &wire)
	result := Project{ID: staticID(wire.ID, "prj_"), Name: displayText(wire.Name, 100), Framework: displayText(wire.Framework, 128)}
	if requestErr != nil {
		return result, requestErr
	}
	if parseErr != nil || decodeErr != nil || result.ID == "" || wire.Name != name || wire.Framework != "" || (teamID != "" && wire.AccountID != "" && wire.AccountID != teamID) {
		return result, staticUnknown(invalidResponse())
	}
	return result, nil
}

// CreateStaticDeployment issues exactly one POST and explicitly publishes the
// first release to production. It never sets forceNew, gitSource, deploymentId,
// projectSettings, environment values, aliases or an unsupported idempotency key.
func (c *Client) CreateStaticDeployment(ctx context.Context, token, teamID string, input StaticDeploymentRequest) (StaticDeploymentResult, error) {
	if err := validateScope(token, teamID); err != nil {
		return StaticDeploymentResult{}, err
	}
	if staticID(input.ProjectID, "prj_") == "" || !staticProjectName(input.Name) || !validProviderID(input.OperationID) || !staticSHA256(input.ArtifactSHA256) {
		return StaticDeploymentResult{}, staticInvalid("vercel static deployment identity is invalid")
	}
	if err := validateStaticFiles(input.Files); err != nil {
		return StaticDeploymentResult{}, err
	}
	type inlineFile struct {
		File     string `json:"file"`
		Data     string `json:"data"`
		Encoding string `json:"encoding"`
	}
	files := make([]inlineFile, 0, len(input.Files))
	for _, file := range input.Files {
		files = append(files, inlineFile{File: file.Path, Data: base64.StdEncoding.EncodeToString(file.Data), Encoding: "base64"})
	}
	body, err := json.Marshal(struct {
		Project string            `json:"project"`
		Name    string            `json:"name"`
		Target  string            `json:"target"`
		Files   []inlineFile      `json:"files"`
		Meta    map[string]string `json:"meta"`
	}{Project: input.ProjectID, Name: input.Name, Target: "production", Files: files, Meta: map[string]string{"cookerOperationId": input.OperationID, "cookerArtifactSha256": input.ArtifactSHA256}})
	if err != nil {
		return StaticDeploymentResult{}, staticInvalid("vercel deployment request could not be encoded")
	}
	data, requestErr := c.staticRequest(ctx, token, http.MethodPost, "/v13/deployments", scopeQuery(teamID), body)
	result, parseErr := staticDeployment(data)
	if requestErr != nil {
		return result, requestErr
	}
	if parseErr != nil || (result.ProjectID != "" && result.ProjectID != input.ProjectID) || (teamID != "" && result.OwnerID != "" && result.OwnerID != teamID) || (result.OperationID != "" && result.OperationID != input.OperationID) || (result.ArtifactSHA256 != "" && result.ArtifactSHA256 != input.ArtifactSHA256) || (result.Target != "" && result.Target != "production") {
		return result, staticUnknown(invalidResponse())
	}
	return result, nil
}

func (c *Client) GetStaticDeployment(ctx context.Context, token, teamID, id string) (StaticDeploymentResult, error) {
	if err := validateScope(token, teamID); err != nil {
		return StaticDeploymentResult{}, err
	}
	if staticID(id, "dpl_") == "" {
		return StaticDeploymentResult{}, staticInvalid("vercel deployment ID is invalid")
	}
	data, requestErr := c.staticRequest(ctx, token, http.MethodGet, "/v13/deployments/"+id, scopeQuery(teamID), nil)
	result, parseErr := staticDeployment(data)
	if requestErr != nil {
		return result, requestErr
	}
	if parseErr != nil || result.ID != id || (teamID != "" && result.OwnerID != "" && result.OwnerID != teamID) {
		return result, invalidResponse()
	}
	return result, nil
}

func (c *Client) staticRequest(ctx context.Context, token, method, endpoint string, query url.Values, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return nil, &APIError{Code: "canceled", Message: "vercel publishing request was canceled before submission"}
	}
	u := apiOrigin + endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, staticInvalid("vercel publishing request could not be created")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Cooker-Vercel-Static/1")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, staticRequestError(method, &APIError{Code: "transport", Message: "vercel publishing response was not received"})
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		// The provider explicitly rejected this request. Never expose its body.
		e := statusError(response.StatusCode, response.Header.Get("Retry-After"), response.Header.Get("X-RateLimit-Reset"))
		if e.Code == "provider_error" {
			e.Message = "vercel rejected the publishing request"
		}
		return nil, e
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return data, staticRequestError(method, statusError(response.StatusCode, response.Header.Get("Retry-After"), response.Header.Get("X-RateLimit-Reset")))
	}
	if readErr != nil {
		return data, staticRequestError(method, &APIError{Code: "response_read", Message: "vercel publishing response was interrupted"})
	}
	if len(data) > maxBodyBytes {
		return data, staticRequestError(method, limitError("vercel publishing response exceeds the supported size"))
	}
	return data, nil
}

func staticRequestError(method string, err error) error {
	if method == http.MethodPost {
		return staticUnknown(err)
	}
	return err
}
func staticUnknown(err error) error { return fmt.Errorf("%w: %w", ErrMutationUnknown, err) }
func staticInvalid(message string) *APIError {
	return &APIError{Code: "invalid_input", Message: message}
}

// Decode one field at a time so an acknowledged ID survives a truncated body,
// an invalid later field, or a later read error. Duplicate keys fail closed.
func staticObject(data []byte) (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	seen := map[string]bool{}
	d := json.NewDecoder(bytes.NewReader(data))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return fields, invalidResponse()
	}
	duplicate := false
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return fields, invalidResponse()
		}
		key, ok := token.(string)
		if !ok {
			return fields, invalidResponse()
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return fields, invalidResponse()
		}
		folded := strings.ToLower(key)
		if seen[folded] {
			duplicate = true
			continue
		}
		seen[folded] = true
		fields[key] = value
	}
	last, err := d.Token()
	if err != nil || last != json.Delim('}') {
		return fields, invalidResponse()
	}
	var extra any
	if d.Decode(&extra) != io.EOF || duplicate {
		return fields, invalidResponse()
	}
	return fields, nil
}

func staticDecode(fields map[string]json.RawMessage, target any) error {
	data, err := json.Marshal(fields)
	if err != nil {
		return invalidResponse()
	}
	return json.Unmarshal(data, target)
}

func staticDeployment(data []byte) (StaticDeploymentResult, error) {
	fields, parseErr := staticObject(data)
	var raw struct {
		ID, ProjectID, OwnerID, ReadyState, State, URL string
		Target                                         *string
		Project                                        *struct{ ID string }
		Team                                           *struct{ ID string }
		Meta                                           map[string]string
	}
	decodeErr := staticDecode(fields, &raw)
	result := StaticDeploymentResult{ID: staticID(raw.ID, "dpl_"), ProjectID: staticID(raw.ProjectID, "prj_"), OwnerID: raw.OwnerID, URL: deploymentURL(raw.URL), OperationID: raw.Meta["cookerOperationId"], ArtifactSHA256: raw.Meta["cookerArtifactSha256"]}
	if raw.Project != nil {
		if result.ProjectID != "" && result.ProjectID != raw.Project.ID {
			parseErr = invalidResponse()
		}
		if result.ProjectID == "" {
			result.ProjectID = staticID(raw.Project.ID, "prj_")
		}
	}
	if raw.Team != nil {
		if result.OwnerID != "" && result.OwnerID != raw.Team.ID {
			parseErr = invalidResponse()
		}
		if result.OwnerID == "" {
			result.OwnerID = raw.Team.ID
		}
	}
	state := raw.ReadyState
	if state == "" {
		state = raw.State
	}
	if state != "" {
		result.State = deploymentState(state)
	}
	if raw.Target != nil {
		result.Target = displayText(*raw.Target, 128)
	} else if _, ok := fields["target"]; ok {
		result.Target = "preview"
	}
	if result.ID == "" || result.State == "" || result.State == "UNKNOWN" {
		parseErr = invalidResponse()
	}
	if (raw.ProjectID != "" && result.ProjectID == "") || (raw.Project != nil && staticID(raw.Project.ID, "prj_") == "") {
		parseErr = invalidResponse()
	}
	if result.OwnerID != "" && !validProviderID(result.OwnerID) {
		result.OwnerID = ""
		parseErr = invalidResponse()
	}
	if result.OperationID != "" && !validProviderID(result.OperationID) {
		result.OperationID = ""
		parseErr = invalidResponse()
	}
	if result.ArtifactSHA256 != "" && !staticSHA256(result.ArtifactSHA256) {
		result.ArtifactSHA256 = ""
		parseErr = invalidResponse()
	}
	if parseErr != nil || decodeErr != nil {
		return result, invalidResponse()
	}
	return result, nil
}

func staticID(value, prefix string) string {
	if validProviderID(value) && strings.HasPrefix(value, prefix) && len(value) > len(prefix) {
		return value
	}
	return ""
}
func staticSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}
func staticProjectName(name string) bool {
	if name == "" || len(name) > 100 || name[0] == '-' || name[len(name)-1] == '-' || strings.Contains(name, "---") {
		return false
	}
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-':
		default:
			return false
		}
	}
	return true
}

func validateStaticFiles(files []StaticFile) error {
	if len(files) == 0 || len(files) > MaxStaticFiles {
		return staticInvalid("vercel static artifact has an invalid file count")
	}
	seen := map[string]bool{}
	total := 0
	index := false
	for _, file := range files {
		if !staticPath(file.Path) {
			return staticInvalid("vercel static artifact contains an unsupported file path")
		}
		key := strings.ToLower(file.Path)
		if seen[key] {
			return staticInvalid("vercel static artifact contains duplicate file paths")
		}
		seen[key] = true
		if file.Path == "index.html" {
			index = true
		}
		if len(file.Data) > MaxStaticFileBytes {
			return staticInvalid("vercel static artifact file exceeds the supported size")
		}
		total += len(file.Data)
		if total > MaxStaticTotalBytes {
			return staticInvalid("vercel static artifact exceeds the supported size")
		}
	}
	if !index {
		return staticInvalid("vercel static artifact must contain index.html")
	}
	return nil
}

func staticPath(value string) bool {
	if value == "" || len(value) > 512 || path.Clean(value) != value || strings.HasPrefix(value, "/") {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '/', ch == '.', ch == '_', ch == '-':
		default:
			return false
		}
	}
	for _, part := range strings.Split(strings.ToLower(value), "/") {
		if strings.HasPrefix(part, ".") || part == "api" || part == "functions" || part == "node_modules" {
			return false
		}
	}
	base := strings.ToLower(path.Base(value))
	switch base {
	case "vercel.json", "now.json", "package.json", "package-lock.json", "deno.json", "composer.json", "requirements.txt", "middleware.js", "proxy.js":
		return false
	}
	if base == "env.example" {
		return true
	}
	switch strings.ToLower(path.Ext(value)) {
	case ".html", ".htm", ".css", ".js", ".mjs", ".json", ".svg", ".png", ".jpg", ".jpeg", ".webp", ".gif", ".ico", ".txt", ".woff", ".woff2", ".ttf", ".otf", ".pdf", ".md", ".yaml", ".yml":
		return true
	default:
		return false
	}
}
