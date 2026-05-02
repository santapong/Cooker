package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/auth"
	"github.com/cooker-ci/cooker/internal/crypto"
	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/secrets/database"
	"github.com/cooker-ci/cooker/internal/store/memory"
)

func init() { gin.SetMode(gin.TestMode) }

// newTestHandler returns a Handler with an in-memory store, an active
// codec, and the database secrets manager wired through. Mirrors the
// production wiring path so secret endpoints exercise the same flow.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	return New(st, codec, database.New(st.Environments, codec))
}

func newRequest(method, target string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func withUser(h gin.HandlerFunc, claims *auth.Claims) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user", claims)
		h(c)
	}
}

func TestApprovePromotion_ViewerForbidden(t *testing.T) {
	h := newTestHandler(t)
	viewer := &auth.Claims{Email: "v@example.com", Roles: []string{string(auth.RoleViewer)}}

	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/approve", withUser(h.ApprovePromotion, viewer))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/approve", map[string]string{"note": "looks good"}))
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestApprovePromotion_ApproverAllowed_UsesClaimsEmail(t *testing.T) {
	h := newTestHandler(t)
	approver := &auth.Claims{Email: "approver@example.com", Roles: []string{string(auth.RoleApprover)}}

	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/approve", withUser(h.ApprovePromotion, approver))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/approve", map[string]string{"approvedBy": "evil@example.com", "note": "shipping"}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["approvedBy"] != "approver@example.com" {
		t.Errorf("expected approvedBy from claims, got %v", body["approvedBy"])
	}
}

func TestSecretRoundTrip_AdminOnly(t *testing.T) {
	h := newTestHandler(t)

	env := &model.Environment{ID: "env1", Name: "prod", PlainVars: map[string]string{"DEBUG": "false"}}
	if err := h.Store.Environments.Create(nil, env); err != nil {
		t.Fatal(err)
	}

	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}
	operator := &auth.Claims{Email: "o@example.com", Roles: []string{string(auth.RoleOperator)}}

	r := gin.New()
	r.PUT("/environments/:id/secrets/:key", withUser(h.PutSecret, admin))
	r.GET("/environments/:id/secrets/:key", withUser(h.RevealSecret, admin))
	r.GET("/ops/environments/:id/secrets/:key", withUser(h.RevealSecret, operator))
	r.GET("/environments", withUser(h.ListEnvironments, operator))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/environments/env1/secrets/DB_PASSWORD", map[string]string{"value": "hunter2"}))
	if w.Code != http.StatusOK {
		t.Fatalf("put: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/environments/env1/secrets/DB_PASSWORD", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("reveal: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["value"] != "hunter2" {
		t.Errorf("round-trip: got %q want hunter2", body["value"])
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ops/environments/env1/secrets/DB_PASSWORD", nil))
	if w.Code != http.StatusForbidden {
		t.Errorf("operator reveal: expected 403, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/environments", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []struct {
		Secrets    map[string]string `json:"secrets"`
		SecretKeys []string          `json:"secretKeys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list size: %d", len(list))
	}
	if list[0].Secrets != nil {
		t.Errorf("list response must never include raw secrets; got %v", list[0].Secrets)
	}
	if len(list[0].SecretKeys) != 1 || list[0].SecretKeys[0] != "DB_PASSWORD" {
		t.Errorf("expected SecretKeys=[DB_PASSWORD]; got %v", list[0].SecretKeys)
	}
}

func TestSecrets_ReturnsServiceUnavailable_WhenNoManager(t *testing.T) {
	h := New(memory.New(), nil, nil)
	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.PUT("/environments/:id/secrets/:key", withUser(h.PutSecret, admin))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/environments/env1/secrets/K", map[string]string{"value": "x"}))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}
