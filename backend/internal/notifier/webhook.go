package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookConfig is the JSON shape stored in Target.Config for kind=webhook.
type WebhookConfig struct {
	URL         string            `json:"url"`
	BearerToken string            `json:"bearerToken,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// WebhookNotifier POSTs the JSON-serialized Event to an arbitrary
// URL. Useful for routing to a downstream automation server, a
// custom Slack-app, or an internal pipeline that consumes raw
// events. The body is the Event struct verbatim so receivers don't
// have to parse channel-specific shapes.
type WebhookNotifier struct {
	http *http.Client
}

func NewWebhookNotifier() *WebhookNotifier {
	return &WebhookNotifier{http: &http.Client{Timeout: 10 * time.Second}}
}

func (w *WebhookNotifier) WithHTTPClient(c *http.Client) *WebhookNotifier {
	w.http = c
	return w
}

func (w *WebhookNotifier) Kind() string { return "webhook" }

func (w *WebhookNotifier) Send(ctx context.Context, target Target, event Event) error {
	var cfg WebhookConfig
	if err := target.DecodeConfig(&cfg); err != nil {
		return fmt.Errorf("webhook: decode config: %w", err)
	}
	if cfg.URL == "" {
		return fmt.Errorf("webhook: target %s missing url", target.ID)
	}
	buf, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("webhook: marshal event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook: status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
