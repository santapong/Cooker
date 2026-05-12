package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
)

func TestApp_CreateAndList_RedactsWebhookSecret(t *testing.T) {
	h := newTestHandler(t)
	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}

	r := gin.New()
	r.POST("/apps", withUser(h.CreateApp, admin))
	r.GET("/apps", withUser(h.ListApps, admin))

	create := model.App{
		Name:       "web",
		GitHubRepo: "acme/web",
		Branch:     "main",
		AutoDeploy: true,
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps", create))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apps", nil))
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list size: %d", len(list))
	}
	if _, present := list[0]["webhookSecret"]; present {
		t.Error("webhookSecret must never appear in list response")
	}
	if hw, _ := list[0]["hasWebhook"].(bool); hw {
		t.Error("hasWebhook should be false for app without webhook")
	}
}

func TestGitHubWebhook_Valid_HMAC_TriggersDeploy(t *testing.T) {
	h := newTestHandler(t)

	// Seed an App and set its webhook secret via the real handler so
	// encryption is exercised.
	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/apps", withUser(h.CreateApp, admin))
	r.PUT("/apps/:id/webhook", withUser(h.SetAppWebhookSecret, admin))
	r.POST("/webhooks/github", h.GitHubWebhook)

	// Create
	create := model.App{Name: "api", GitHubRepo: "acme/api", Branch: "main", AutoDeploy: true}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps", create))
	var got model.App
	_ = json.Unmarshal(w.Body.Bytes(), &got)

	// Rotate webhook secret
	secret := "github-hook-secret"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/apps/"+got.ID+"/webhook", map[string]string{"secret": secret}))
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", w.Code, w.Body.String())
	}

	// Send a push event
	payload := []byte(`{"ref":"refs/heads/main","after":"deadbeef","repository":{"full_name":"acme/api"}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", sig)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("webhook: %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["commit"] != "deadbeef" || body["branch"] != "main" {
		t.Errorf("body: %v", body)
	}
}

func TestGitHubWebhook_Rejects_BadSignature(t *testing.T) {
	h := newTestHandler(t)
	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/apps", withUser(h.CreateApp, admin))
	r.PUT("/apps/:id/webhook", withUser(h.SetAppWebhookSecret, admin))
	r.POST("/webhooks/github", h.GitHubWebhook)

	create := model.App{Name: "svc", GitHubRepo: "acme/svc", Branch: "main", AutoDeploy: true}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps", create))
	var got model.App
	_ = json.Unmarshal(w.Body.Bytes(), &got)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/apps/"+got.ID+"/webhook", map[string]string{"secret": "right"}))

	payload := []byte(`{"ref":"refs/heads/main","repository":{"full_name":"acme/svc"}}`)
	mac := hmac.New(sha256.New, []byte("wrong"))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", sig)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGitHubWebhook_UnknownRepo_Returns204(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.POST("/webhooks/github", h.GitHubWebhook)

	payload := []byte(`{"ref":"refs/heads/main","repository":{"full_name":"nobody/here"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}
