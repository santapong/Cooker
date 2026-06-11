package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/service"
)

// withConfigServices attaches the Settings registry + cluster config
// services wired to the same store + secrets backend the handler uses.
// newTestHandler doesn't set these, so any test exercising the persist
// path must call this first (mirrors withHostService).
func withConfigServices(h *Handler) *Handler {
	h.Registries = service.NewRegistryConfigService(h.Store, h.Secrets)
	h.Clusters = service.NewClusterConfigService(h.Store, h.Secrets)
	return h
}

// TestRegistryConfig_CreateListDelete_RoundTrip is the core HS26-05-04
// assertion: an added registry survives a subsequent List (it used to
// vanish — the handler returned a canned 201 and List returned []).
func TestRegistryConfig_CreateListDelete_RoundTrip(t *testing.T) {
	h := withConfigServices(newTestHandler(t))
	r := gin.New()
	r.POST("/settings/registries", h.AddRegistryConfig)
	r.GET("/settings/registries", h.ListRegistryConfigs)
	r.DELETE("/settings/registries/:id", h.DeleteRegistryConfig)

	// List starts empty.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/registries", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var initial []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &initial)
	if len(initial) != 0 {
		t.Fatalf("expected empty list, got %d", len(initial))
	}

	// Add one with a password.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/settings/registries", map[string]any{
		"name":     "ghcr-prod",
		"url":      "ghcr.io/org",
		"username": "ci-bot",
		"password": "ghp_supersecrettoken",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id in create response")
	}

	// List now shows it — the persistence round-trip that HS26-05-04 lacked.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/registries", nil))
	var listed []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed) != 1 {
		t.Fatalf("expected 1 registry after create, got %d: %s", len(listed), w.Body.String())
	}
	if listed[0]["name"] != "ghcr-prod" || listed[0]["url"] != "ghcr.io/org" {
		t.Errorf("listed registry fields wrong: %+v", listed[0])
	}
	if hp, _ := listed[0]["hasPassword"].(bool); !hp {
		t.Errorf("hasPassword should be true, got %v", listed[0]["hasPassword"])
	}

	// Delete removes it.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/settings/registries/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/registries", nil))
	var afterDelete []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &afterDelete)
	if len(afterDelete) != 0 {
		t.Fatalf("expected empty list after delete, got %d", len(afterDelete))
	}
}

// TestRegistryConfig_CreateRedactsPassword is the security assertion:
// no Create/List response may contain the password bytes or the
// internal secrets reference.
func TestRegistryConfig_CreateRedactsPassword(t *testing.T) {
	h := withConfigServices(newTestHandler(t))
	r := gin.New()
	r.POST("/settings/registries", h.AddRegistryConfig)
	r.GET("/settings/registries", h.ListRegistryConfigs)

	const secret = "ghp_supersecrettoken_DO_NOT_LEAK"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/settings/registries", map[string]any{
		"name":     "ghcr-prod",
		"url":      "ghcr.io/org",
		"username": "ci-bot",
		"password": secret,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatalf("Create response leaked password:\n%s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "passwordRef") || strings.Contains(w.Body.String(), "password_ref") {
		t.Fatalf("Create response leaked password ref:\n%s", w.Body.String())
	}
	// The write-only "password" key must not be echoed either.
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if _, ok := created["password"]; ok {
		t.Fatalf("Create response echoed password field:\n%s", w.Body.String())
	}

	// Same redaction on List.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/registries", nil))
	if strings.Contains(w.Body.String(), secret) {
		t.Fatalf("List response leaked password:\n%s", w.Body.String())
	}
}

// TestRegistryConfig_AnonymousNoPassword: a registry without a password
// persists fine and reports hasPassword=false (anonymous registries are
// a legitimate case and must not require a secrets backend).
func TestRegistryConfig_AnonymousNoPassword(t *testing.T) {
	h := withConfigServices(newTestHandler(t))
	r := gin.New()
	r.POST("/settings/registries", h.AddRegistryConfig)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/settings/registries", map[string]any{
		"name": "public-mirror",
		"url":  "mirror.example.com",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if hp, _ := created["hasPassword"].(bool); hp {
		t.Errorf("hasPassword should be false for anonymous registry, got true")
	}
}

// TestClusterConfig_CreateListDelete_RoundTrip mirrors the registry
// round-trip for clusters, asserting kubeconfig redaction.
func TestClusterConfig_CreateListDelete_RoundTrip(t *testing.T) {
	h := withConfigServices(newTestHandler(t))
	r := gin.New()
	r.POST("/settings/clusters", h.AddClusterConfig)
	r.GET("/settings/clusters", h.ListClusterConfigs)
	r.DELETE("/settings/clusters/:id", h.DeleteClusterConfig)

	const kubeconfig = "apiVersion: v1\nkind: Config\nclusters:\n- cluster:\n    token: SECRET_KUBE_TOKEN"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/settings/clusters", map[string]any{
		"name":       "prod-eu",
		"context":    "prod",
		"kubeconfig": kubeconfig,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "SECRET_KUBE_TOKEN") {
		t.Fatalf("Create response leaked kubeconfig:\n%s", w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id in create response")
	}
	if hc, _ := created["hasCredentials"].(bool); !hc {
		t.Errorf("hasCredentials should be true, got %v", created["hasCredentials"])
	}

	// List shows it (persistence round-trip) and stays redacted.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/clusters", nil))
	var listed []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed) != 1 {
		t.Fatalf("expected 1 cluster after create, got %d", len(listed))
	}
	if listed[0]["context"] != "prod" {
		t.Errorf("listed cluster context wrong: %+v", listed[0])
	}
	if strings.Contains(w.Body.String(), "SECRET_KUBE_TOKEN") {
		t.Fatalf("List response leaked kubeconfig:\n%s", w.Body.String())
	}

	// Delete removes it.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/settings/clusters/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/clusters", nil))
	var afterDelete []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &afterDelete)
	if len(afterDelete) != 0 {
		t.Fatalf("expected empty list after delete, got %d", len(afterDelete))
	}
}
