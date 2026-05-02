package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/cooker-ci/cooker/internal/auth"
)

// rateLimiter is a per-user token-bucket limiter intended for the
// most expensive endpoints (pipeline runs, image builds, app deploys).
// It is in-memory and per-process: deployments running multiple
// replicas should disable this and rely on edge-level rate limiting
// at the ingress / WAF, where state is shared.
type rateLimiter struct {
	rps      rate.Limit
	burst    int
	mu       sync.Mutex
	buckets  map[string]*rate.Limiter
	lastSeen map[string]time.Time
}

// newRateLimiter constructs a limiter at perMinute requests per
// minute with the given burst capacity. Pass perMinute<=0 to make
// the limiter a no-op (used when COOKER_RATE_LIMIT_ENABLED=false).
func newRateLimiter(perMinute int, burst int) *rateLimiter {
	rps := rate.Limit(0)
	if perMinute > 0 {
		rps = rate.Limit(float64(perMinute) / 60.0)
	}
	rl := &rateLimiter{
		rps:      rps,
		burst:    burst,
		buckets:  make(map[string]*rate.Limiter),
		lastSeen: make(map[string]time.Time),
	}
	if perMinute > 0 {
		// Sweep idle buckets every 10 minutes so a long-running
		// process doesn't accumulate one bucket per ever-seen user.
		go rl.gc(10 * time.Minute)
	}
	return rl
}

func (rl *rateLimiter) limiterFor(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.lastSeen[key] = time.Now()
	if l, ok := rl.buckets[key]; ok {
		return l
	}
	l := rate.NewLimiter(rl.rps, rl.burst)
	rl.buckets[key] = l
	return l
}

func (rl *rateLimiter) gc(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-interval)
		rl.mu.Lock()
		for k, t := range rl.lastSeen {
			if t.Before(cutoff) {
				delete(rl.buckets, k)
				delete(rl.lastSeen, k)
			}
		}
		rl.mu.Unlock()
	}
}

// middleware returns a Gin handler that consumes one token per
// request from the caller's bucket. The bucket key is the OIDC
// subject claim, falling back to the request remote address for
// unauthenticated paths (which shouldn't be rate-limited via this
// middleware in practice — it is mounted under /api/v1 only).
func (rl *rateLimiter) middleware() gin.HandlerFunc {
	if rl.rps == 0 {
		// Disabled: pass through.
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		key := rateLimitKey(c)
		if !rl.limiterFor(key).Allow() {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded; try again in a moment",
			})
			return
		}
		c.Next()
	}
}

func rateLimitKey(c *gin.Context) string {
	if claims := auth.GetUser(c); claims != nil && claims.Subject != "" {
		return "user:" + claims.Subject
	}
	return "ip:" + c.ClientIP()
}
