package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// withUser returns a middleware that injects a Claims with the
// given subject so rateLimitKey picks it up as user:<sub>.
func withUser(sub string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user", &auth.Claims{Subject: sub})
		c.Next()
	}
}

func newRateLimitedRouter(perMin, burst int, sub string) *gin.Engine {
	r := gin.New()
	rl := newRateLimiter(perMin, burst)
	r.Use(withUser(sub))
	r.Use(rl.middleware())
	r.POST("/run", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func hammer(t *testing.T, r *gin.Engine, n int) (ok, throttled int) {
	t.Helper()
	for i := 0; i < n; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/run", nil)
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			throttled++
		default:
			t.Fatalf("unexpected status %d", w.Code)
		}
	}
	return
}

func TestRateLimit_BurstAllowed(t *testing.T) {
	// Burst 3 means the first 3 requests must pass.
	r := newRateLimitedRouter(60, 3, "alice")
	ok, throttled := hammer(t, r, 3)
	if ok != 3 || throttled != 0 {
		t.Errorf("first 3 within burst: ok=%d throttled=%d, want ok=3", ok, throttled)
	}
}

func TestRateLimit_ThrottlesAfterBurst(t *testing.T) {
	// 60 req/min = 1 rps, burst 2. The first 2 pass, the next 8
	// fired in tight succession should mostly fail.
	r := newRateLimitedRouter(60, 2, "alice")
	ok, throttled := hammer(t, r, 10)
	if ok < 2 || ok > 3 {
		t.Errorf("expected ~2 ok within burst, got ok=%d throttled=%d", ok, throttled)
	}
	if throttled < 7 {
		t.Errorf("expected the rest throttled, got ok=%d throttled=%d", ok, throttled)
	}
}

func TestRateLimit_PerUser(t *testing.T) {
	// alice exhausts her burst — bob shouldn't be affected.
	rl := newRateLimiter(60, 1)
	r := gin.New()
	r.Use(rl.middleware())
	r.POST("/run", func(c *gin.Context) { c.Status(http.StatusOK) })

	call := func(sub string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/run", nil)
		// Inject user context per-request (bypassing the router's middleware chain).
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = req
		ctx.Set("user", &auth.Claims{Subject: sub})
		// Run the rate-limit middleware directly.
		rl.middleware()(ctx)
		if !ctx.IsAborted() {
			ctx.Status(http.StatusOK)
		}
		return ctx.Writer.Status()
	}

	if got := call("alice"); got != http.StatusOK {
		t.Fatalf("alice first call: got %d, want 200", got)
	}
	if got := call("alice"); got != http.StatusTooManyRequests {
		t.Fatalf("alice second call: got %d, want 429", got)
	}
	if got := call("bob"); got != http.StatusOK {
		t.Errorf("bob's first call should be unaffected by alice: got %d, want 200", got)
	}
}

func TestRateLimit_DisabledIsPassthrough(t *testing.T) {
	r := newRateLimitedRouter(0, 0, "alice")
	ok, throttled := hammer(t, r, 50)
	if ok != 50 || throttled != 0 {
		t.Errorf("disabled limiter should pass everything: ok=%d throttled=%d", ok, throttled)
	}
}

// hammerFromIP fires n requests through r all originating from remoteAddr,
// tallying 200s vs 429s. Used to exercise the per-IP webhook limiter.
func hammerFromIP(t *testing.T, r *gin.Engine, remoteAddr string, n int) (ok, throttled int) {
	t.Helper()
	for i := 0; i < n; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/webhooks/github", nil)
		req.RemoteAddr = remoteAddr
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			throttled++
		default:
			t.Fatalf("unexpected status %d", w.Code)
		}
	}
	return
}

// TestWebhookRateLimit_ThrottlesPerIP proves the dedicated unauthenticated
// webhook limiter (C-webhook-logs) bounds a single hostile source IP: after
// the burst is spent, further requests get 429 and never reach the handler.
func TestWebhookRateLimit_ThrottlesPerIP(t *testing.T) {
	rl := newRateLimiter(60, 2) // 1 rps, burst 2
	defer rl.Close()
	r := gin.New()
	r.POST("/webhooks/github", rl.webhookMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })

	ok, throttled := hammerFromIP(t, r, "9.9.9.9:1111", 10)
	if ok < 2 || ok > 3 {
		t.Errorf("expected ~2 ok within burst, got ok=%d throttled=%d", ok, throttled)
	}
	if throttled < 7 {
		t.Errorf("expected the rest throttled, got ok=%d throttled=%d", ok, throttled)
	}
}

// TestWebhookRateLimit_PerIPIsolation proves one hostile IP exhausting its
// bucket does not throttle a different (legitimate) source IP.
func TestWebhookRateLimit_PerIPIsolation(t *testing.T) {
	rl := newRateLimiter(60, 1) // 1 rps, burst 1
	defer rl.Close()
	r := gin.New()
	r.POST("/webhooks/github", rl.webhookMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })

	// Attacker IP spends its single-token bucket and then gets throttled.
	if ok, throttled := hammerFromIP(t, r, "9.9.9.9:1111", 5); ok != 1 || throttled != 4 {
		t.Fatalf("attacker IP: ok=%d throttled=%d, want ok=1 throttled=4", ok, throttled)
	}
	// A different IP is unaffected — its first request still passes.
	if ok, _ := hammerFromIP(t, r, "1.2.3.4:2222", 1); ok != 1 {
		t.Errorf("distinct IP should be unaffected, got ok=%d", ok)
	}
}

// TestWebhookRateLimit_DisabledIsPassthrough proves a zero-budget limiter
// (COOKER_WEBHOOK_RATE_LIMIT_* disabled path) passes everything through.
func TestWebhookRateLimit_DisabledIsPassthrough(t *testing.T) {
	rl := newRateLimiter(0, 0)
	defer rl.Close()
	r := gin.New()
	r.POST("/webhooks/github", rl.webhookMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })
	if ok, throttled := hammerFromIP(t, r, "9.9.9.9:1111", 50); ok != 50 || throttled != 0 {
		t.Errorf("disabled webhook limiter should pass everything: ok=%d throttled=%d", ok, throttled)
	}
}

// hammerLocalAuth drives the unauthenticated signin path from a fixed source
// IP, counting 200/429 like hammerFromIP but for the local-auth route.
func hammerLocalAuth(t *testing.T, r *gin.Engine, remoteAddr string, n int) (ok, throttled int) {
	t.Helper()
	for i := 0; i < n; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/local/signin", nil)
		req.RemoteAddr = remoteAddr
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			throttled++
		default:
			t.Fatalf("unexpected status %d", w.Code)
		}
	}
	return
}

// TestLocalAuthRateLimit_ThrottlesPerIP proves the credential-endpoint limiter
// (CWE-307) bounds brute-force from a single source IP after the burst.
func TestLocalAuthRateLimit_ThrottlesPerIP(t *testing.T) {
	rl := newRateLimiter(localAuthRateLimitPerMinute, localAuthRateLimitBurst)
	defer rl.Close()
	r := gin.New()
	r.POST("/api/v1/auth/local/signin", rl.localAuthMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })

	ok, throttled := hammerLocalAuth(t, r, "9.9.9.9:1111", 20)
	if ok < localAuthRateLimitBurst || ok > localAuthRateLimitBurst+1 {
		t.Errorf("expected ~%d ok within burst, got ok=%d throttled=%d", localAuthRateLimitBurst, ok, throttled)
	}
	if throttled == 0 {
		t.Errorf("expected the rest throttled, got ok=%d throttled=%d", ok, throttled)
	}
}

// TestLocalAuthRateLimit_PerIPIsolation proves one hostile IP exhausting its
// bucket does not throttle a distinct (legitimate) source IP.
func TestLocalAuthRateLimit_PerIPIsolation(t *testing.T) {
	rl := newRateLimiter(60, 1) // 1 rps, burst 1
	defer rl.Close()
	r := gin.New()
	r.POST("/api/v1/auth/local/signin", rl.localAuthMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })

	if ok, throttled := hammerLocalAuth(t, r, "9.9.9.9:1111", 5); ok != 1 || throttled != 4 {
		t.Fatalf("attacker IP: ok=%d throttled=%d, want ok=1 throttled=4", ok, throttled)
	}
	if ok, _ := hammerLocalAuth(t, r, "1.2.3.4:2222", 1); ok != 1 {
		t.Errorf("distinct IP should be unaffected, got ok=%d", ok)
	}
}

func TestRateLimit_KeyByUserOrIP(t *testing.T) {
	r := gin.New()
	c1, _ := gin.CreateTestContext(httptest.NewRecorder())
	c1.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c1.Request.RemoteAddr = "1.2.3.4:5678"
	r.NoRoute(func(c *gin.Context) {})
	if got := rateLimitKey(c1); got != "ip:1.2.3.4" {
		t.Errorf("unauthenticated key: got %q, want ip:1.2.3.4", got)
	}
	c1.Set("user", &auth.Claims{Subject: "alice"})
	if got := rateLimitKey(c1); got != "user:alice" {
		t.Errorf("authenticated key: got %q, want user:alice", got)
	}
}
