package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSlackNotifierSendShape(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := SlackConfig{WebhookURL: srv.URL, Channel: "#alerts", Username: "cooker"}
	cfgJSON, _ := json.Marshal(cfg)
	target := Target{ID: "t1", Kind: "slack", Enabled: true, Config: cfgJSON}

	n := NewSlackNotifier()
	err := n.Send(context.Background(), target, Event{Type: EventRunFailed, PipelineName: "x"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content-type=%q", gotContentType)
	}
	var payload slackPayload
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Channel != "#alerts" || payload.Username != "cooker" {
		t.Fatalf("payload missing config fields: %+v", payload)
	}
	if payload.Text == "" {
		t.Fatal("empty text")
	}
}

func TestSlackNotifierMissingURL(t *testing.T) {
	n := NewSlackNotifier()
	err := n.Send(context.Background(), Target{ID: "t1", Kind: "slack"}, Event{Type: EventRunFailed})
	if err == nil {
		t.Fatal("want missing-URL error")
	}
}

func TestSlackNotifierNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := SlackConfig{WebhookURL: srv.URL}
	cfgJSON, _ := json.Marshal(cfg)
	n := NewSlackNotifier()
	err := n.Send(context.Background(), Target{ID: "t1", Kind: "slack", Config: cfgJSON}, Event{Type: EventRunFailed})
	if err == nil {
		t.Fatal("want non-2xx error")
	}
}
