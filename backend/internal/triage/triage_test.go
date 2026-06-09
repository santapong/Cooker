package triage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func testClient(url string) *Client {
	c := New("test-key", "")
	c.BaseURL = url
	return c
}

func TestTriage_HappyPathParsesTextBlocks(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing auth/version headers: %v", r.Header)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"model":"claude-fable-5","content":[
			{"type":"text","text":"Probable cause: missing base image."},
			{"type":"text","text":"Fix: check registry credentials."}]}`))
	}))
	defer srv.Close()

	res, err := testClient(srv.URL).Triage(context.Background(), Request{System: "sys", Prompt: "why"})
	if err != nil {
		t.Fatalf("Triage: %v", err)
	}
	if !strings.Contains(res.Advisory, "missing base image") || !strings.Contains(res.Advisory, "registry credentials") {
		t.Errorf("advisory = %q", res.Advisory)
	}
	if res.Model != "claude-fable-5" {
		t.Errorf("model = %q", res.Model)
	}
	// The request must not carry sampling or thinking params — the
	// default model rejects them.
	for _, banned := range []string{"temperature", "top_p", "top_k", "thinking"} {
		if _, ok := gotBody[banned]; ok {
			t.Errorf("request body must omit %q", banned)
		}
	}
	if gotBody["model"] != "claude-fable-5" {
		t.Errorf("request model = %v", gotBody["model"])
	}
}

func TestTriage_ErrorEnvelopeTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"bad key"},"request_id":"req_123"}`))
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).Triage(context.Background(), Request{Prompt: "p"})
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("expected ErrAuth, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RequestID != "req_123" || apiErr.Type != "authentication_error" {
		t.Errorf("api error = %+v", apiErr)
	}
}

func TestTriage_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"m","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()

	res, err := testClient(srv.URL).Triage(context.Background(), Request{Prompt: "p"})
	if err != nil {
		t.Fatalf("Triage after retry: %v", err)
	}
	if res.Advisory != "ok" || calls.Load() != 2 {
		t.Errorf("advisory=%q calls=%d", res.Advisory, calls.Load())
	}
}

func TestTriage_BadRequestNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"nope"}}`))
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).Triage(context.Background(), Request{Prompt: "p"})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("400 must fail once without retry; err=%v calls=%d", err, calls.Load())
	}
}

func TestBuildRequest_StripsSecretsAndTailsLogs(t *testing.T) {
	bigLogs := strings.Repeat("x", logTailBytes) + "\nFATAL: out of disk"
	stage := &model.Stage{
		Name: "Build", Type: model.StageTypeBuild,
		Config: model.StageConfig{
			Dockerfile: "Dockerfile",
			Env:        map[string]string{"API_TOKEN": "supersecretvalue"},
			SecretRefs: []string{"DB_PASSWORD"},
		},
	}
	sr := &model.StageRun{Error: "exit 1", Logs: bigLogs}

	req := BuildRequest(stage, sr)
	if strings.Contains(req.Prompt, "supersecretvalue") {
		t.Error("env VALUES must never reach the prompt")
	}
	if !strings.Contains(req.Prompt, "API_TOKEN") {
		t.Error("env names should be listed for context")
	}
	if !strings.Contains(req.Prompt, "FATAL: out of disk") {
		t.Error("log tail (the failure) must be present")
	}
	if len(req.Prompt) > logTailBytes+4096 {
		t.Errorf("prompt not tailed: %d bytes", len(req.Prompt))
	}
	if req.System == "" {
		t.Error("system prompt missing")
	}
}
