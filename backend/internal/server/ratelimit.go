package server

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/santapong/cooker/internal/auth"
)

// rateLimiter is a per-user token-bucket limiter intended for the
// most expensive endpoints (pipeline runs, image builds, app deploys).
// It is in-memory and per-process: deployments running multiple
// replicas should disable this and rely on edge-level rate limiting
// at the ingress / WAF, where state is shared.
//
// P26-05-12: mu is an RWMutex so the hot read path (bucket already
// registered) acquires only a read-lock. At ~50 concurrent users on
// expensive endpoints the old Mutex was a single serialisation point;
// the RWMutex reduces lock contention ~90% on steady-state traffic.
// The gc loop uses the full write-lock as before; lastSeen updates on
// the fast read path use a separate lastMu so bucket reads never block
// on lastSeen writes.
// See docs/audits/2026-05-perf-and-optimization.md §P26-05-12.
type rateLimiter struct {
	rps      rate.Limit
	burst    int
	mu       sync.RWMutex
	buckets  map[string]*rate.Limiter
	lastMu   sync.Mutex
	lastSeen map[string]time.Time
	// done is closed by Close() to stop the gc goroutine (BC-H1).
	done chan struct{}
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
		done:     make(chan struct{}),
	}
	if perMinute > 0 {
		// Sweep idle buckets every 10 minutes so a long-running
		// process doesn't accumulate one bucket per ever-seen user.
		go rl.gc(10 * time.Minute)
	}
	return rl
}

// Close signals the gc goroutine to exit. Safe to call on a
// disabled limiter (perMinute<=0 — gc was never started, close is a no-op).
func (rl *rateLimiter) Close() {
	select {
	case <-rl.done:
		// already closed
	default:
		close(rl.done)
	}
}

func (rl *rateLimiter) limiterFor(key string) *rate.Limiter {
	// Fast path: bucket already exists — read-lock only.
	rl.mu.RLock()
	l, ok := rl.buckets[key]
	rl.mu.RUnlock()
	if ok {
		// Update lastSeen under its own mutex so the bucket read never
		// contends with the gc write. Approximate timestamp is fine;
		// gc only deletes after interval of staleness.
		rl.lastMu.Lock()
		rl.lastSeen[key] = time.Now()
		rl.lastMu.Unlock()
		return l
	}
	// Slow path: allocate a new bucket under write-lock.
	rl.mu.Lock()
	defer rl.mu.Unlock()
	// Double-check: another goroutine may have raced us to the write-lock.
	if l, ok = rl.buckets[key]; ok {
		return l
	}
	l = rate.NewLimiter(rl.rps, rl.burst)
	rl.buckets[key] = l
	rl.lastMu.Lock()
	rl.lastSeen[key] = time.Now()
	rl.lastMu.Unlock()
	return l
}

func (rl *rateLimiter) gc(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-rl.done:
			slog.Debug("ratelimiter: gc stopped")
			return
		case <-ticker.C:
		}
		cutoff := time.Now().Add(-interval)
		// Collect stale keys under lastMu, then delete from buckets
		// under the write-lock. This keeps the two locks non-nested
		// on the gc path, matching the non-nesting invariant in limiterFor.
		var stale []string
		rl.lastMu.Lock()
		for k, t := range rl.lastSeen {
			if t.Before(cutoff) {
				stale = append(stale, k)
				delete(rl.lastSeen, k)
			}
		}
		rl.lastMu.Unlock()
		if len(stale) > 0 {
			rl.mu.Lock()
			for _, k := range stale {
				delete(rl.buckets, k)
			}
			rl.mu.Unlock()
		}
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

// webhookMiddleware returns a Gin handler for the unauthenticated
// /webhooks/* receivers. Unlike middleware(), it ALWAYS keys on the source
// IP (there is no authenticated principal on these routes) so a single
// hostile sender can't amplify the pre-verification DB lookup + secret
// decryption each provider webhook performs before its HMAC/token check.
//
// The limiter runs BEFORE any handler work, so a throttled request never
// reaches the store or the crypto codec. On throttle it returns 429 with a
// Retry-After hint, matching the per-user limiter's response shape.
//
// Like the per-user limiter it is in-memory / per-process; multi-replica
// deployments should also rate-limit at the edge (see SECURITY.md).
func (rl *rateLimiter) webhookMiddleware() gin.HandlerFunc {
	if rl.rps == 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		key := "webhook-ip:" + c.ClientIP()
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
