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

// DiscordConfig is the JSON shape stored in Target.Config for kind=discord.
type DiscordConfig struct {
	WebhookURL string `json:"webhookUrl"`
	Username   string `json:"username,omitempty"`
	AvatarURL  string `json:"avatarUrl,omitempty"`
}

// DiscordNotifier posts webhook payloads to Discord using a
// rich embed (color-coded by event status).
type DiscordNotifier struct {
	http *http.Client
}

func NewDiscordNotifier() *DiscordNotifier {
	return &DiscordNotifier{http: &http.Client{Timeout: 10 * time.Second}}
}

func (d *DiscordNotifier) WithHTTPClient(c *http.Client) *DiscordNotifier {
	d.http = c
	return d
}

func (d *DiscordNotifier) Kind() string { return "discord" }

type discordEmbed struct {
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	URL         string    `json:"url,omitempty"`
	Color       int       `json:"color,omitempty"`
	Timestamp   time.Time `json:"timestamp,omitempty"`
}

type discordPayload struct {
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []discordEmbed `json:"embeds"`
}

// colorForEvent picks a Discord embed color (24-bit RGB) per
// event type. Green for success, red for failure, gray for
// cancelled, orange for build failure.
func colorForEvent(t EventType) int {
	switch t {
	case EventRunSucceeded, EventDeploySucceeded:
		return 0x2ECC71 // green
	case EventRunFailed, EventDeployFailed:
		return 0xE74C3C // red
	case EventBuildFailed:
		return 0xE67E22 // orange
	case EventRunCancelled:
		return 0x95A5A6 // gray
	}
	return 0
}

func (d *DiscordNotifier) Send(ctx context.Context, target Target, event Event) error {
	var cfg DiscordConfig
	if err := target.DecodeConfig(&cfg); err != nil {
		return fmt.Errorf("discord: decode config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("discord: target %s missing webhookUrl", target.ID)
	}
	payload := discordPayload{
		Username:  cfg.Username,
		AvatarURL: cfg.AvatarURL,
		Embeds: []discordEmbed{{
			Title:       event.Title(),
			Description: event.Body(),
			URL:         event.URL,
			Color:       colorForEvent(event.Type),
			Timestamp:   event.Timestamp,
		}},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("discord: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("discord: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("discord: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord: status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
