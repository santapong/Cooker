package auth

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/config"
)

func TestNewMiddleware_Disabled(t *testing.T) {
	m, err := NewMiddleware(context.Background(), config.OIDCConfig{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("middleware is nil")
	}
}

func TestNewMiddleware_EnabledMissingIssuer(t *testing.T) {
	_, err := NewMiddleware(context.Background(), config.OIDCConfig{Enabled: true})
	if err == nil {
		t.Fatal("expected error for missing issuer")
	}
	if !strings.Contains(err.Error(), "COOKER_OIDC_ISSUER_URL") {
		t.Errorf("error message does not mention missing issuer: %v", err)
	}
}

func TestNewMiddleware_EnabledMissingClientID(t *testing.T) {
	_, err := NewMiddleware(context.Background(), config.OIDCConfig{
		Enabled:   true,
		IssuerURL: "https://auth.example.com",
	})
	if err == nil {
		t.Fatal("expected error for missing client ID")
	}
	if !strings.Contains(err.Error(), "COOKER_OIDC_CLIENT_ID") {
		t.Errorf("error message does not mention missing client ID: %v", err)
	}
}

func TestDevHandler_SetsAdminUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m, err := NewMiddleware(context.Background(), config.OIDCConfig{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := m.Handler()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/anything", nil)

	h(c)

	claims := GetUser(c)
	if claims == nil {
		t.Fatal("expected claims to be set on context")
	}
	if claims.Subject != "dev-user" {
		t.Errorf("Subject = %q, want dev-user", claims.Subject)
	}
	found := false
	for _, r := range claims.Roles {
		if r == string(RoleAdmin) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected admin role in dev user, got %v", claims.Roles)
	}
}

func TestBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		header  string
		wantOK  bool
		wantTok string
	}{
		{"missing", "", false, ""},
		{"bearer lowercase", "bearer abc", true, "abc"},
		{"bearer mixed", "Bearer abc", true, "abc"},
		{"malformed scheme", "Token abc", false, ""},
		{"no value", "Bearer", false, ""},
		{"trailing space", "Bearer ", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/x", nil)
			if tt.header != "" {
				c.Request.Header.Set("Authorization", tt.header)
			}
			tok, ok := bearerToken(c)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tok != tt.wantTok {
				t.Errorf("token = %q, want %q", tok, tt.wantTok)
			}
		})
	}
}
