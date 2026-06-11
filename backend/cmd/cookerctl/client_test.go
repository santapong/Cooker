package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

// newTestClient wires a client at a test server's URL.
func newTestClient(srv *httptest.Server, token string) *client {
	return newClient(srv.URL, token)
}

func TestClient_InjectsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(srv, "ck_secrettoken")
	if _, err := c.ListPipelines(context.Background()); err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if gotAuth != "Bearer ck_secrettoken" {
		t.Fatalf("Authorization header = %q, want bearer with token", gotAuth)
	}
}

func TestClient_Unauthorized_WrapsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"authentication failed"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, "ck_bad")
	_, err := c.ListPipelines(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !errors.Is(err, errUnauthorized) {
		t.Fatalf("401 error not wrapped with errUnauthorized: %v", err)
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("error envelope message lost: %v", err)
	}
}

func TestClient_ErrorEnvelopeDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"stage type \"bogus\" is not valid"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, "ck_tok")
	_, err := c.ImportPipeline(context.Background(), []byte("apiVersion: cooker.dev/v1\n"))
	if err == nil {
		t.Fatal("expected 400 error")
	}
	var ae *apiError
	if !errors.As(err, &ae) {
		t.Fatalf("want *apiError, got %T: %v", err, err)
	}
	if ae.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", ae.Status)
	}
	if !strings.Contains(ae.Message, "is not valid") {
		t.Fatalf("message = %q", ae.Message)
	}
}

// TestClient_ExportImportRoundTrip walks the export → import path against a
// fake server that echoes the exported YAML back through import, asserting
// the YAML body is sent verbatim with the application/yaml content type and
// that the server-minted pipeline id is returned.
func TestClient_ExportImportRoundTrip(t *testing.T) {
	const exportedYAML = "apiVersion: cooker.dev/v1\nkind: Pipeline\nmetadata:\n  name: demo\nspec:\n  stages: []\n  edges: []\n"
	var importBody string
	var importContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pipelines/p1/export":
			w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
			_, _ = w.Write([]byte(exportedYAML))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/pipelines/import":
			b := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(b)
			importBody = string(b)
			importContentType = r.Header.Get("Content-Type")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"p2","name":"demo","version":0}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv, "ck_tok")
	ctx := context.Background()

	doc, err := c.ExportPipeline(ctx, "p1")
	if err != nil {
		t.Fatalf("ExportPipeline: %v", err)
	}
	if string(doc) != exportedYAML {
		t.Fatalf("export body mismatch:\n got %q\nwant %q", doc, exportedYAML)
	}

	p, err := c.ImportPipeline(ctx, doc)
	if err != nil {
		t.Fatalf("ImportPipeline: %v", err)
	}
	if importBody != exportedYAML {
		t.Fatalf("import did not send exported YAML verbatim:\n got %q\nwant %q", importBody, exportedYAML)
	}
	if !strings.HasPrefix(importContentType, "application/yaml") {
		t.Fatalf("import Content-Type = %q, want application/yaml", importContentType)
	}
	if p.ID != "p2" {
		t.Fatalf("imported pipeline id = %q, want p2", p.ID)
	}
}

func TestClient_RunPipeline_SendsIdempotencyKey(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"run1","pipelineId":"p1","status":"pending"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, "ck_tok")
	run, err := c.RunPipeline(context.Background(), "p1", runOptions{idempotencyKey: "fixed-key-123"})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if gotKey != "fixed-key-123" {
		t.Fatalf("Idempotency-Key = %q, want fixed-key-123", gotKey)
	}
	if run.ID != "run1" || run.Status != model.RunStatusPending {
		t.Fatalf("unexpected run: %+v", run)
	}
}

func TestClient_Version_ParsesBuildFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			t.Errorf("path = %q, want /version", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"build_version":"v1.0.0","build_sha":"abc123","build_time":"2026-06-11T00:00:00Z","go_version":"go1.25.0"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, "")
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v.BuildVersion != "v1.0.0" || v.BuildSHA != "abc123" {
		t.Fatalf("parsed version wrong: %+v", v)
	}
}
