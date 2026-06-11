package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
)

// tokenRouter wires the three token routes under a fixed authenticated
// identity (claims), mirroring how production gates them (RBAC lives in the
// service, so no route-level role middleware here).
func tokenRouter(h *Handler, claims *auth.Claims) *gin.Engine {
	r := gin.New()
	r.POST("/tokens", withUser(h.CreateAPIToken, claims))
	r.GET("/tokens", withUser(h.ListAPITokens, claims))
	r.DELETE("/tokens/:id", withUser(h.DeleteAPIToken, claims))
	return r
}

func TestAPIToken_Handler_CreateListDeleteRoundTrip(t *testing.T) {
	h := newTestHandler(t)
	admin := &auth.Claims{Subject: "admin-sub", Email: "admin@x.com", Roles: []string{string(auth.RoleAdmin)}}
	r := tokenRouter(h, admin)

	// CREATE → 201, plaintext returned exactly once.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/tokens", map[string]any{
		"name": "ci-deploy",
		"role": "operator",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Token    string         `json:"token"`
		APIToken model.APIToken `json:"apiToken"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !strings.HasPrefix(created.Token, model.APITokenPrefix) {
		t.Errorf("plaintext token should carry %q prefix; got %q", model.APITokenPrefix, created.Token)
	}
	if created.APIToken.ID == "" {
		t.Errorf("created apiToken has no id")
	}
	// CRITICAL: the hash must never be serialised to the client.
	if strings.Contains(w.Body.String(), "hash") {
		t.Errorf("create response leaks the hash field: %s", w.Body.String())
	}
	if created.APIToken.Role != "operator" {
		t.Errorf("role = %q, want operator", created.APIToken.Role)
	}

	// LIST → the new token is present; plaintext is NOT (only at creation).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/tokens", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), created.Token) {
		t.Errorf("list response leaks the plaintext token")
	}
	if !strings.Contains(w.Body.String(), created.APIToken.DisplayPrefix) {
		t.Errorf("list response missing the display prefix")
	}

	// DELETE → 200.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/tokens/"+created.APIToken.ID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}

	// LIST again → empty.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/tokens", nil))
	var listed struct {
		Tokens []model.APIToken `json:"tokens"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed.Tokens) != 0 {
		t.Errorf("expected 0 tokens after delete; got %d", len(listed.Tokens))
	}
}

func TestAPIToken_Handler_RoleCapForbidden(t *testing.T) {
	h := newTestHandler(t)
	viewer := &auth.Claims{Subject: "v-sub", Email: "v@x.com", Roles: []string{string(auth.RoleViewer)}}
	r := tokenRouter(h, viewer)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/tokens", map[string]any{
		"name": "sneaky-admin",
		"role": "admin",
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer minting admin should be 403; got %d %s", w.Code, w.Body.String())
	}
}

func TestAPIToken_Handler_TokenCannotMintToken(t *testing.T) {
	h := newTestHandler(t)
	// A token-authenticated request: Subject carries the token: prefix.
	tokenActor := &auth.Claims{Subject: auth.TokenSubjectPrefix + "abc", Email: "creator@x.com", Roles: []string{string(auth.RoleAdmin)}}
	r := tokenRouter(h, tokenActor)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/tokens", map[string]any{
		"name": "spawn",
		"role": "viewer",
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("token-authenticated create should be 403; got %d %s", w.Code, w.Body.String())
	}
}

func TestAPIToken_Handler_DeleteOthersIsNotFoundForNonAdmin(t *testing.T) {
	h := newTestHandler(t)
	// Alice creates a token.
	alice := &auth.Claims{Subject: "alice", Email: "alice@x.com", Roles: []string{string(auth.RoleOperator)}}
	w := httptest.NewRecorder()
	tokenRouter(h, alice).ServeHTTP(w, newRequest(http.MethodPost, "/tokens", map[string]any{"name": "a", "role": "viewer"}))
	var created struct {
		APIToken model.APIToken `json:"apiToken"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// Bob (non-admin) tries to delete it → 404 (not 403), so ids aren't
	// probeable.
	bob := &auth.Claims{Subject: "bob", Email: "bob@x.com", Roles: []string{string(auth.RoleOperator)}}
	w = httptest.NewRecorder()
	tokenRouter(h, bob).ServeHTTP(w, newRequest(http.MethodDelete, "/tokens/"+created.APIToken.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner delete should be 404; got %d %s", w.Code, w.Body.String())
	}
}

func TestAPIToken_Handler_AdminListAll(t *testing.T) {
	h := newTestHandler(t)
	// Two creators.
	alice := &auth.Claims{Subject: "alice", Email: "alice@x.com", Roles: []string{string(auth.RoleOperator)}}
	bob := &auth.Claims{Subject: "bob", Email: "bob@x.com", Roles: []string{string(auth.RoleOperator)}}
	for _, c := range []*auth.Claims{alice, bob} {
		w := httptest.NewRecorder()
		tokenRouter(h, c).ServeHTTP(w, newRequest(http.MethodPost, "/tokens", map[string]any{"name": "t", "role": "viewer"}))
		if w.Code != http.StatusCreated {
			t.Fatalf("seed create: %d %s", w.Code, w.Body.String())
		}
	}

	// Non-admin ?all=true → 403.
	w := httptest.NewRecorder()
	tokenRouter(h, alice).ServeHTTP(w, newRequest(http.MethodGet, "/tokens?all=true", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin all=true should be 403; got %d", w.Code)
	}

	// Admin ?all=true → sees both.
	admin := &auth.Claims{Subject: "admin", Email: "admin@x.com", Roles: []string{string(auth.RoleAdmin)}}
	w = httptest.NewRecorder()
	tokenRouter(h, admin).ServeHTTP(w, newRequest(http.MethodGet, "/tokens?all=true", nil))
	var listed struct {
		Tokens []model.APIToken `json:"tokens"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed.Tokens) != 2 {
		t.Fatalf("admin all=true should see 2 tokens; got %d", len(listed.Tokens))
	}
}
