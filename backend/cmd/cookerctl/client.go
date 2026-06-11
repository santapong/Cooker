// Command cookerctl is a kubectl-style CLI for driving a Cooker server
// over its REST API with an API token. It covers the core automation
// loop: list / export / import / run pipelines and follow runs.
//
// See docs/user-guide/guides/cli.md for the install and usage guide.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// errUnauthorized is returned (wrapped) whenever the server answers 401.
// The dispatcher unwraps it to print the "create a token" hint exactly
// once, rather than threading the hint through every call site.
var errUnauthorized = errors.New("unauthorized")

// apiError carries a non-2xx response. The server's error envelope is
// always {"error": "..."}; Message is that string when present, else a
// generic status line. Body is retained for non-JSON error bodies.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.Status)
}

// client is a thin typed wrapper over the Cooker REST API. It injects the
// bearer token on every request and never logs or echoes the token value.
type client struct {
	baseURL string
	token   string
	http    *http.Client
}

// newClient builds a client for baseURL using token for bearer auth.
// baseURL is normalised to drop a trailing slash so path joins are clean.
func newClient(baseURL, token string) *client {
	return &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// versionInfo mirrors the /version handler's JSON shape
// (internal/server/version.go), which is build_* keyed — NOT the
// OpenAPI VersionInfo schema. The handler is the source of truth.
type versionInfo struct {
	BuildVersion string `json:"build_version"`
	BuildSHA     string `json:"build_sha"`
	BuildTime    string `json:"build_time"`
	GoVersion    string `json:"go_version"`
}

// stageLogs mirrors GetStageLogs' actual JSON ({"logs": "..."}), which
// omits the stageId the OpenAPI StageLogs schema lists. The caller
// already knows the stage id from the request path.
type stageLogs struct {
	Logs string `json:"logs"`
}

// runResult is the request-side knob for triggering a run: an optional
// idempotency key the server replays on (internal/server/middleware_idempotency.go).
type runOptions struct {
	idempotencyKey string
}

// do issues an authenticated request. body, when non-nil, is sent with
// the given contentType. The decoded non-2xx envelope becomes an
// *apiError; a 401 is additionally wrapped with errUnauthorized so the
// dispatcher can attach the token hint. out, when non-nil, receives the
// JSON-decoded 2xx body; pass nil to ignore the body.
func (c *client) do(ctx context.Context, method, path, contentType string, body []byte, hdr map[string]string, out any) error {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	// The token is attached here and nowhere else; it is never written to
	// stdout/stderr or included in error strings.
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		ae := &apiError{Status: resp.StatusCode, Message: decodeErrEnvelope(raw)}
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w: %s", errUnauthorized, ae.Error())
		}
		return ae
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response %s %s: %w", method, path, err)
		}
	}
	return nil
}

// decodeErrEnvelope extracts the message from the server's {"error": ...}
// envelope, falling back to a trimmed raw body when the response is not
// the expected JSON shape (e.g. a proxy 502 in HTML).
func decodeErrEnvelope(raw []byte) string {
	var env struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Error != "" {
		return env.Error
	}
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// Version fetches the server build metadata from the unauthenticated
// /version endpoint.
func (c *client) Version(ctx context.Context) (versionInfo, error) {
	var v versionInfo
	err := c.do(ctx, http.MethodGet, "/version", "", nil, nil, &v)
	return v, err
}

// ListPipelines returns every pipeline visible to the caller.
func (c *client) ListPipelines(ctx context.Context) ([]model.Pipeline, error) {
	var out []model.Pipeline
	err := c.do(ctx, http.MethodGet, "/api/v1/pipelines", "", nil, nil, &out)
	return out, err
}

// GetPipeline fetches a single pipeline by id.
func (c *client) GetPipeline(ctx context.Context, id string) (model.Pipeline, error) {
	var p model.Pipeline
	err := c.do(ctx, http.MethodGet, "/api/v1/pipelines/"+id, "", nil, nil, &p)
	return p, err
}

// ExportPipeline returns the pipeline-as-code YAML document for id. The
// body is YAML, not JSON, so it is returned verbatim.
func (c *client) ExportPipeline(ctx context.Context, id string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/pipelines/"+id+"/export", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/yaml")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("export pipeline: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		ae := &apiError{Status: resp.StatusCode, Message: decodeErrEnvelope(raw)}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("%w: %s", errUnauthorized, ae.Error())
		}
		return nil, ae
	}
	return raw, nil
}

// ImportPipeline creates a new pipeline from a pipeline-as-code YAML
// document and returns the server-assigned pipeline (with its fresh ID).
func (c *client) ImportPipeline(ctx context.Context, yamlDoc []byte) (model.Pipeline, error) {
	var p model.Pipeline
	err := c.do(ctx, http.MethodPost, "/api/v1/pipelines/import", "application/yaml", yamlDoc, nil, &p)
	return p, err
}

// RunPipeline triggers a run. The optional idempotency key is sent as the
// Idempotency-Key header; the server replays the cached 202 on a repeat.
func (c *client) RunPipeline(ctx context.Context, id string, opts runOptions) (model.PipelineRun, error) {
	var run model.PipelineRun
	var hdr map[string]string
	if opts.idempotencyKey != "" {
		hdr = map[string]string{"Idempotency-Key": opts.idempotencyKey}
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/pipelines/"+id+"/run", "", nil, hdr, &run)
	return run, err
}

// ListRuns returns run summaries (newest first) for a pipeline. Per-stage
// logs are omitted by the server on this path.
func (c *client) ListRuns(ctx context.Context, pipelineID string) ([]model.PipelineRun, error) {
	var out []model.PipelineRun
	err := c.do(ctx, http.MethodGet, "/api/v1/pipelines/"+pipelineID+"/runs", "", nil, nil, &out)
	return out, err
}

// GetRun fetches a single run (with its stage runs) for a pipeline.
func (c *client) GetRun(ctx context.Context, pipelineID, runID string) (model.PipelineRun, error) {
	var run model.PipelineRun
	err := c.do(ctx, http.MethodGet, "/api/v1/pipelines/"+pipelineID+"/runs/"+runID, "", nil, nil, &run)
	return run, err
}

// StageLogs fetches the final REST snapshot of one stage's logs.
func (c *client) StageLogs(ctx context.Context, pipelineID, runID, stageID string) (string, error) {
	var sl stageLogs
	err := c.do(ctx, http.MethodGet,
		"/api/v1/pipelines/"+pipelineID+"/runs/"+runID+"/logs/"+stageID, "", nil, nil, &sl)
	return sl.Logs, err
}
