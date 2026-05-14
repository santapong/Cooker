package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookNotifierPostsEventAndAuthHeader(t *testing.T) {
	var got []byte
	var gotAuth string
	var gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Cooker-Source")
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := WebhookConfig{
		URL:         srv.URL,
		BearerToken: "tok-123",
		Headers:     map[string]string{"X-Cooker-Source": "test"},
	}
	cfgJSON, _ := json.Marshal(cfg)
	target := Target{ID: "w1", Kind: "webhook", Enabled: true, Config: cfgJSON}

	n := NewWebhookNotifier()
	err := n.Send(context.Background(), target, Event{Type: EventRunFailed, RunID: "r1"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotCustom != "test" {
		t.Fatalf("custom header=%q", gotCustom)
	}
	var event Event
	if err := json.Unmarshal(got, &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.RunID != "r1" {
		t.Fatalf("event round-trip lost RunID: %+v", event)
	}
}

func TestWebhookNotifierMissingURL(t *testing.T) {
	n := NewWebhookNotifier()
	err := n.Send(context.Background(), Target{ID: "w1", Kind: "webhook"}, Event{Type: EventRunFailed})
	if err == nil {
		t.Fatal("want missing-URL error")
	}
}
