package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/config"
)

// TestNewMiddleware_BootSurvivesUnreachableIssuer asserts that a wrong
// issuer URL does NOT prevent the middleware from being constructed —
// discovery now happens lazily on the first authenticated request.
func TestNewMiddleware_BootSurvivesUnreachableIssuer(t *testing.T) {
	m, err := NewMiddleware(context.Background(), config.OIDCConfig{
		Enabled:   true,
		IssuerURL: "http://127.0.0.1:1", // black hole
		ClientID:  "cooker",
	})
	if err != nil {
		t.Fatalf("NewMiddleware should succeed even when IdP is unreachable: %v", err)
	}
	if m == nil {
		t.Fatal("middleware is nil")
	}
	if _, ok := m.LastJWKSRefresh(); ok {
		t.Fatal("LastJWKSRefresh should report invalid before any verify")
	}
}

// TestEnsureProvider_RetryCooldown asserts that consecutive failed
// discovery attempts do not actually re-dial the IdP — the second
// call returns immediately with a cool-down error.
func TestEnsureProvider_RetryCooldown(t *testing.T) {
	m, err := NewMiddleware(context.Background(), config.OIDCConfig{
		Enabled:   true,
		IssuerURL: "http://127.0.0.1:1",
		ClientID:  "cooker",
	})
	if err != nil {
		t.Fatalf("NewMiddleware: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := m.ensureProvider(ctx); err == nil {
		t.Fatal("expected first discovery to fail")
	}
	start := time.Now()
	err = m.ensureProvider(context.Background())
	if err == nil {
		t.Fatal("expected cooled-down call to fail without re-dialing")
	}
	if !strings.Contains(err.Error(), "cooling down") {
		t.Fatalf("expected cooldown error, got: %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("cooled-down call took %s; should be near-instant", d)
	}
}

// TestHandler_503OnUnreachableIdP asserts that an authenticated
// request with a Bearer token returns 503 with Retry-After when the
// IdP cannot be reached, instead of crashing the boot path.
func TestHandler_503OnUnreachableIdP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m, err := NewMiddleware(context.Background(), config.OIDCConfig{
		Enabled:   true,
		IssuerURL: "http://127.0.0.1:1",
		ClientID:  "cooker",
	})
	if err != nil {
		t.Fatalf("NewMiddleware: %v", err)
	}
	r := gin.New()
	r.GET("/x", m.Handler(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 503")
	}
}
