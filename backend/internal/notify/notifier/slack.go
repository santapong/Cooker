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

// SlackConfig is the JSON shape stored in Target.Config for kind=slack.
type SlackConfig struct {
	// WebhookURL is the Slack incoming-webhook URL
	// (https://hooks.slack.com/services/...).
	WebhookURL string `json:"webhookUrl"`
	// Channel optionally overrides the destination channel encoded
	// in the webhook URL; useful when a single webhook is shared.
	Channel string `json:"channel,omitempty"`
	// Username/IconEmoji control the bot face for this target.
	Username  string `json:"username,omitempty"`
	IconEmoji string `json:"iconEmoji,omitempty"`
}

// SlackNotifier posts incoming-webhook payloads to Slack.
type SlackNotifier struct {
	http *http.Client
}

// NewSlackNotifier returns a SlackNotifier with a default 10s
// HTTP client. Pass a custom client when the operator needs to
// route through a proxy / present client certs.
func NewSlackNotifier() *SlackNotifier {
	return &SlackNotifier{http: &http.Client{Timeout: 10 * time.Second}}
}

// WithHTTPClient swaps the HTTP client (tests).
func (s *SlackNotifier) WithHTTPClient(c *http.Client) *SlackNotifier {
	s.http = c
	return s
}

// Kind implements Notifier.
func (s *SlackNotifier) Kind() string { return "slack" }

// slackPayload is the wire shape for Slack incoming webhooks.
type slackPayload struct {
	Text      string `json:"text"`
	Channel   string `json:"channel,omitempty"`
	Username  string `json:"username,omitempty"`
	IconEmoji string `json:"icon_emoji,omitempty"`
}

// Send implements Notifier. Posts a text-mode payload — simpler
// than Slack Blocks and adequate for v1; channels can upgrade to
// blocks in a follow-up without changing the public API.
func (s *SlackNotifier) Send(ctx context.Context, target Target, event Event) error {
	var cfg SlackConfig
	if err := target.DecodeConfig(&cfg); err != nil {
		return fmt.Errorf("slack: decode config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("slack: target %s missing webhookUrl", target.ID)
	}
	body := event.Title() + "\n" + event.Body()
	payload := slackPayload{
		Text:      body,
		Channel:   cfg.Channel,
		Username:  cfg.Username,
		IconEmoji: cfg.IconEmoji,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("slack: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack: status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
