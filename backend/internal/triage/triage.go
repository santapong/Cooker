// Package triage calls the Anthropic Messages API to produce an
// advisory explanation of a failed pipeline stage (roadmap M4).
// Opt-in: the server constructs a Client only when
// COOKER_AI_TRIAGE_ENABLED=true and ANTHROPIC_API_KEY is set. The
// key stays server-side; the frontend only ever sees the advisory
// text. Modeled on the keepsave outbound client (30s-class timeout,
// TLS >= 1.2, typed HTTP errors).
package triage

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/santapong/cooker/internal/retry"
)

// DefaultModel is used when COOKER_AI_TRIAGE_MODEL is unset.
const DefaultModel = "claude-fable-5"

const (
	defaultBaseURL = "https://api.anthropic.com"
	// apiVersion is the required anthropic-version header value.
	apiVersion = "2023-06-01"
	// maxTokens bounds the advisory length; triage answers are short.
	maxTokens = 1024
	// requestTimeout bounds one Messages call. Generation of ~1k
	// tokens comfortably fits; a hung connection doesn't pin the
	// handler past the route's own deadline.
	requestTimeout = 90 * time.Second
)

// Sentinel errors for errors.Is classification at the handler.
var (
	// ErrRateLimited maps 429 — the operator's Anthropic quota is hot.
	ErrRateLimited = errors.New("triage: rate limited")
	// ErrOverloaded maps 529/5xx — transient upstream trouble.
	ErrOverloaded = errors.New("triage: upstream overloaded")
	// ErrAuth maps 401/403 — bad or revoked API key.
	ErrAuth = errors.New("triage: authentication failed")
)

// APIError is the Anthropic error envelope with classification
// support: errors.Is(err, ErrRateLimited) etc. work through it.
type APIError struct {
	Status    int
	Type      string
	Message   string
	RequestID string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("triage: api %d %s: %s (request_id=%s)", e.Status, e.Type, e.Message, e.RequestID)
}

// Is lets the sentinel classification flow through errors.Is.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrRateLimited:
		return e.Status == http.StatusTooManyRequests
	case ErrOverloaded:
		return e.Status >= 500
	case ErrAuth:
		return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	}
	return false
}

// Request is the triage input assembled by BuildRequest.
type Request struct {
	// System is the fixed triage system prompt.
	System string
	// Prompt is the composed user content (stage summary + error +
	// log tail). Secrets are stripped by BuildRequest.
	Prompt string
}

// Result is the advisory outcome.
type Result struct {
	// Advisory is the model's plain-text explanation + suggested next
	// steps. Advisory only — Cooker never acts on it automatically.
	Advisory string
	// Model echoes the model that produced the advisory.
	Model string
}

// Client talks to the Anthropic Messages API.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// New builds a Client with the hardened transport. model empty falls
// back to DefaultModel.
func New(apiKey, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &Client{
		BaseURL: defaultBaseURL,
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: requestTimeout, Transport: t},
	}
}

// Wire shapes (subset of the Messages API we use). The request omits
// temperature/top_p/top_k and the thinking param entirely — the
// default model rejects all of them, and omission works everywhere.
type messagesRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	System    string        `json:"system,omitempty"`
	Messages  []wireMessage `json:"messages"`
}

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type errorEnvelope struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	RequestID string `json:"request_id"`
}

// Triage sends the composed prompt and returns the advisory. One
// retry on transient failures (429/5xx) with the package backoff;
// context errors are terminal.
func (c *Client) Triage(ctx context.Context, req Request) (Result, error) {
	if c == nil || c.APIKey == "" {
		return Result{}, errors.New("triage: client not configured")
	}
	var out Result
	policy := retry.Policy{
		MaxAttempts: 2,
		Initial:     2 * time.Second,
		Max:         5 * time.Second,
		IsTransient: func(err error) bool {
			if retry.IsContextErr(err) {
				return false
			}
			return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrOverloaded)
		},
	}
	err := retry.Do(ctx, policy, func(ctx context.Context) error {
		res, callErr := c.call(ctx, req)
		if callErr != nil {
			return callErr
		}
		out = res
		return nil
	})
	return out, err
}

func (c *Client) call(ctx context.Context, req Request) (Result, error) {
	body, err := json.Marshal(messagesRequest{
		Model:     c.Model,
		MaxTokens: maxTokens,
		System:    req.System,
		Messages:  []wireMessage{{Role: "user", Content: req.Prompt}},
	})
	if err != nil {
		return Result{}, fmt.Errorf("triage: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("triage: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("triage: request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, fmt.Errorf("triage: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var env errorEnvelope
		_ = json.Unmarshal(raw, &env)
		return Result{}, &APIError{
			Status:    resp.StatusCode,
			Type:      env.Error.Type,
			Message:   env.Error.Message,
			RequestID: env.RequestID,
		}
	}

	var msg messagesResponse
	if err := json.Unmarshal(raw, &msg); err != nil {
		return Result{}, fmt.Errorf("triage: decode response: %w", err)
	}
	advisory := ""
	for _, block := range msg.Content {
		if block.Type == "text" {
			if advisory != "" {
				advisory += "\n"
			}
			advisory += block.Text
		}
	}
	if advisory == "" {
		return Result{}, errors.New("triage: response carried no text content")
	}
	return Result{Advisory: advisory, Model: msg.Model}, nil
}
