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
