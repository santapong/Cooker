// Package keepsave delegates secret storage to a KeepSave server
// (https://github.com/santapong/keepsave). The HTTP client below is a
// scoped subset of the published KeepSave Go SDK at
// github.com/santapong/KeepSave/sdks/go; once that ships as a real
// Go module (it currently has no go.mod), this file should be
// replaced with an SDK import. The surface is intentionally aligned.
package keepsave

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is a thin REST client for KeepSave's secrets endpoints.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient returns a Client with a default 30s timeout. KeepSave
// authenticates via X-API-Key for agent/server use; that is the only
// auth mode Cooker exercises.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// secretDTO matches KeepSave's Secret JSON shape (subset).
type secretDTO struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id,omitempty"`
	EnvironmentID string `json:"environment_id,omitempty"`
	Key           string `json:"key"`
	Value         string `json:"value,omitempty"`
	Version       int    `json:"version,omitempty"`
}

type listResponse struct {
	Secrets []secretDTO `json:"secrets"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

// HTTPError is returned for non-2xx responses. The handler layer can
// type-switch on it to map status codes to HTTP responses.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("keepsave: %d %s", e.Status, e.Message)
}

// ListSecrets returns all secrets in (projectID, environment).
func (c *Client) ListSecrets(ctx context.Context, projectID, environment string) ([]secretDTO, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/secrets?environment=%s", projectID, url.QueryEscape(environment))
	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var resp listResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode list: %w", err)
	}
	return resp.Secrets, nil
}

// CreateSecret creates a new secret under (projectID, environment).
func (c *Client) CreateSecret(ctx context.Context, projectID, key, value, environment string) error {
	path := fmt.Sprintf("/api/v1/projects/%s/secrets", projectID)
	_, err := c.do(ctx, http.MethodPost, path, map[string]string{
		"key":         key,
		"value":       value,
		"environment": environment,
	})
	return err
}

// UpdateSecret replaces the value of an existing secret by ID.
func (c *Client) UpdateSecret(ctx context.Context, projectID, secretID, value string) error {
	path := fmt.Sprintf("/api/v1/projects/%s/secrets/%s", projectID, secretID)
	_, err := c.do(ctx, http.MethodPut, path, map[string]string{"value": value})
	return err
}

// DeleteSecret removes a secret by ID. 404 is treated as success
// because the caller's intent (key should not exist) is met.
func (c *Client) DeleteSecret(ctx context.Context, projectID, secretID string) error {
	path := fmt.Sprintf("/api/v1/projects/%s/secrets/%s", projectID, secretID)
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	if he, ok := err.(*HTTPError); ok && he.Status == http.StatusNotFound {
		return nil
	}
	return err
}

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var br io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode: %w", err)
		}
		br = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, br)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		var env apiErrorEnvelope
		_ = json.Unmarshal(respBody, &env)
		msg := env.Error.Message
		if msg == "" {
			msg = string(respBody)
		}
		return nil, &HTTPError{Status: resp.StatusCode, Message: msg}
	}
	return respBody, nil
}
