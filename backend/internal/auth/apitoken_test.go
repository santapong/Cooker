package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

// seedToken mints a token, persists it into the store, and returns the
// plaintext credential plus the stored model.
func seedToken(t *testing.T, st store.APITokenStore, role string, expiresAt *time.Time) (string, *model.APIToken) {
	t.Helper()
	plaintext, hash, prefix, err := model.GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	tok := &model.APIToken{
		ID:             "tok-" + role,
		Name:           "ci-" + role,
		DisplayPrefix:  prefix,
		Hash:           hash,
		Role:           role,
		CreatedBySub:   "creator-sub",
		CreatedByEmail: "creator@x.com",
		ExpiresAt:      expiresAt,
	}
	if err := st.Create(context.Background(), tok); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return plaintext, tok
}

// tokenMiddleware builds a Middleware with the API-token path enabled over
// the given store. OIDC and local auth are left off so the test isolates
// the token branch (dev-mode dispatcher).
func tokenMiddleware(t *testing.T, st store.APITokenStore) *Middleware {
	t.Helper()
	m, err := NewMiddleware(context.Background(), config.OIDCConfig{Enabled: false})
	if err != nil {
		t.Fatalf("NewMiddleware: %v", err)
	}
	m.EnableAPITokens(NewAPITokenAuthenticator(st))
	return m
}

func whoamiRouter(m *Middleware) *gin.Engine {
	r := gin.New()
	r.GET("/whoami", m.Handler(), func(c *gin.Context) {
		u := GetUser(c)
		if u == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no user"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"sub": u.Subject, "name": u.Name, "roles": u.Roles})
	})
	return r
}

func TestMiddleware_AcceptsValidAPIToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := memory.New().APITokens
	plaintext, tok := seedToken(t, st, "operator", nil)
	r := whoamiRouter(tokenMiddleware(t, st))

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, TokenSubjectPrefix+tok.ID) {
		t.Errorf("expected subject %q in body; got %s", TokenSubjectPrefix+tok.ID, body)
	}
	if !strings.Contains(body, `"operator"`) {
		t.Errorf("expected role operator in body; got %s", body)
	}
}

func TestMiddleware_RejectsExpiredAPIToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := memory.New().APITokens
	past := time.Now().Add(-time.Hour)
	plaintext, _ := seedToken(t, st, "admin", &past)
	r := whoamiRouter(tokenMiddleware(t, st))

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token; got %d", rec.Code)
	}
	// Generic body — no oracle (S26-05-01 posture).
	if !strings.Contains(rec.Body.String(), "authentication failed") {
		t.Errorf("expected generic body; got %s", rec.Body.String())
	}
}

func TestMiddleware_RejectsRevokedAPIToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := memory.New().APITokens
	plaintext, tok := seedToken(t, st, "viewer", nil)
	// Revoke = delete the row.
	if err := st.Delete(context.Background(), tok.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	r := whoamiRouter(tokenMiddleware(t, st))

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked token; got %d", rec.Code)
	}
}

func TestMiddleware_RejectsWrongSecretAPIToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := memory.New().APITokens
	_, _ = seedToken(t, st, "operator", nil)
	r := whoamiRouter(tokenMiddleware(t, st))

	// A `ck_`-prefixed credential that was never issued: right shape,
	// wrong secret → must not authenticate.
	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+model.APITokenPrefix+"this-is-not-a-real-token-secret")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for forged token; got %d", rec.Code)
	}
}

// TestMiddleware_APITokenPathLeavesLocalJWTUntouched proves the token
// branch only engages on the `ck_` prefix: a valid local JWT presented to
// a middleware that ALSO has the token path enabled still authenticates via
// the local issuer, unchanged.
func TestMiddleware_APITokenPathLeavesLocalJWTUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := memory.New().APITokens

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
	m.EnableAPITokens(NewAPITokenAuthenticator(st))

	jwt, _, err := issuer.Issue("user-1", "u@x.com", "User", "operator")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	r := whoamiRouter(m)

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("local JWT should still authenticate; got %d, body=%s", rec.Code, rec.Body.String())
	}
	// The local-auth subject (not a token: subject) proves the local path ran.
	if !strings.Contains(rec.Body.String(), `"user-1"`) {
		t.Errorf("expected local-auth subject user-1; got %s", rec.Body.String())
	}
}

// countingTokenStore wraps a store and records TouchLastUsed calls so the
// throttle can be asserted deterministically.
type countingTokenStore struct {
	store.APITokenStore
	mu      sync.Mutex
	touched int
}

func (c *countingTokenStore) TouchLastUsed(ctx context.Context, id string, ts time.Time) error {
	c.mu.Lock()
	c.touched++
	c.mu.Unlock()
	return c.APITokenStore.TouchLastUsed(ctx, id, ts)
}

func (c *countingTokenStore) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.touched
}

// TestAPITokenAuthenticator_TouchThrottle confirms last_used_at is written
// at most once per throttle window even under many authenticated calls.
func TestAPITokenAuthenticator_TouchThrottle(t *testing.T) {
	base := memory.New().APITokens
	cs := &countingTokenStore{APITokenStore: base}
	plaintext, _ := seedToken(t, cs, "operator", nil)

	a := NewAPITokenAuthenticator(cs)
	a.async = false // synchronous so the count is deterministic

	// Authenticate ten times in tight succession: one write only.
	for i := 0; i < 10; i++ {
		if _, ok := a.Authenticate(context.Background(), plaintext); !ok {
			t.Fatalf("authenticate %d: expected ok", i)
		}
	}
	if got := cs.count(); got != 1 {
		t.Fatalf("expected exactly 1 last_used write under throttle; got %d", got)
	}

	// Advance past the window → a second write is allowed.
	a.now = func() time.Time { return time.Now().Add(2 * lastUsedThrottle) }
	if _, ok := a.Authenticate(context.Background(), plaintext); !ok {
		t.Fatal("authenticate after window: expected ok")
	}
	if got := cs.count(); got != 2 {
		t.Fatalf("expected 2 last_used writes after window; got %d", got)
	}
}
