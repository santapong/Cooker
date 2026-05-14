package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscordNotifierEmbedShape(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgJSON, _ := json.Marshal(DiscordConfig{WebhookURL: srv.URL})
	target := Target{ID: "d1", Kind: "discord", Enabled: true, Config: cfgJSON}

	n := NewDiscordNotifier()
	err := n.Send(context.Background(), target, Event{Type: EventDeployFailed, AppName: "web", Environment: "prod"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var payload discordPayload
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Embeds) != 1 {
		t.Fatalf("embeds=%d", len(payload.Embeds))
	}
	if payload.Embeds[0].Color != 0xE74C3C {
		t.Fatalf("color=%x want red (0xE74C3C) for deploy-failed", payload.Embeds[0].Color)
	}
	if payload.Embeds[0].Title == "" {
		t.Fatal("empty title")
	}
}

func TestDiscordColorPerEvent(t *testing.T) {
	cases := map[EventType]int{
		EventRunSucceeded:    0x2ECC71,
		EventDeploySucceeded: 0x2ECC71,
		EventRunFailed:       0xE74C3C,
		EventDeployFailed:    0xE74C3C,
		EventBuildFailed:     0xE67E22,
		EventRunCancelled:    0x95A5A6,
	}
	for e, want := range cases {
		if got := colorForEvent(e); got != want {
			t.Errorf("%s: color=%x want %x", e, got, want)
		}
	}
}
