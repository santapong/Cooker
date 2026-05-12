package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/config"
)

func TestMiddleware_AcceptsLocalToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m, err := NewMiddleware(context.Background(), config.OIDCConfig{Enabled: false})
	if err != nil {
		t.Fatalf("NewMiddleware: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	issuer, err := local.NewIssuer(key, time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	m.EnableLocalAuth(issuer)

	tok, _, err := issuer.Issue("user-1", "u@x.com", "User", "operator")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := gin.New()
	r.GET("/whoami", m.Handler(), func(c *gin.Context) {
		u := GetUser(c)
		if u == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no user"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"sub": u.Subject, "email": u.Email, "roles": u.Roles})
	})

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestMiddleware_RejectsTamperedLocalToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m, _ := NewMiddleware(context.Background(), config.OIDCConfig{Enabled: false})
	key := make([]byte, 32)
	issuer, _ := local.NewIssuer(key, time.Minute)
	m.EnableLocalAuth(issuer)

	tok, _, _ := issuer.Issue("u", "e@x.com", "n", "viewer")

	r := gin.New()
	r.GET("/whoami", m.Handler(), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+tok+"extra")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on tampered local token; got %d", rec.Code)
	}
}

func TestMiddleware_NonLocalNonOIDCRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m, _ := NewMiddleware(context.Background(), config.OIDCConfig{Enabled: false})
	key := make([]byte, 32)
	issuer, _ := local.NewIssuer(key, time.Minute)
	m.EnableLocalAuth(issuer)

	r := gin.New()
	r.GET("/whoami", m.Handler(), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer some-random-string-that-is-not-a-jwt")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when OIDC disabled and token is not local; got %d", rec.Code)
	}
}

// S26-05-01: confirm the verify-failure body does not echo upstream
// library detail. Any drift here would re-introduce the oracle the
// audit closed.
func TestMiddleware_TamperedLocalTokenReturnsGenericBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m, _ := NewMiddleware(context.Background(), config.OIDCConfig{Enabled: false})
	key := make([]byte, 32)
	issuer, _ := local.NewIssuer(key, time.Minute)
	m.EnableLocalAuth(issuer)

	tok, _, _ := issuer.Issue("u", "e@x.com", "n", "viewer")

	r := gin.New()
	r.GET("/whoami", m.Handler(), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+tok+"extra")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401; got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "authentication failed") {
		t.Fatalf("expected generic body, got: %s", body)
	}
	// Should NOT contain library diagnostic strings.
	for _, bad := range []string{"signature", "malformed", "kid", "iss", "aud"} {
		if strings.Contains(strings.ToLower(body), bad) {
			t.Fatalf("body leaks upstream diagnostic %q: %s", bad, body)
		}
	}
}
