package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	redisrate "github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

// redisRateLimiter is a multi-replica-safe rate limiter that backs
// onto Redis using the GCRA algorithm via go-redis/redis_rate. It
// satisfies the same middleware shape as the in-memory limiter and
// is selected when COOKER_RATE_LIMIT_BACKEND=redis.
type redisRateLimiter struct {
	limiter   *redisrate.Limiter
	perMinute int
	burst     int
}

func newRedisRateLimiter(client *redis.Client, perMinute, burst int) *redisRateLimiter {
	return &redisRateLimiter{
		limiter:   redisrate.NewLimiter(client),
		perMinute: perMinute,
		burst:     burst,
	}
}

func (rl *redisRateLimiter) middleware() gin.HandlerFunc {
	if rl.perMinute <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	limit := redisrate.PerMinute(rl.perMinute)
	if rl.burst > 0 {
		limit.Burst = rl.burst
	}
	return func(c *gin.Context) {
		key := rateLimitKey(c)
		res, err := rl.limiter.Allow(c.Request.Context(), key, limit)
		if err != nil {
			// Fail open: a redis blip should not lock users out.
			c.Next()
			return
		}
		if res.Allowed == 0 {
			retry := int(res.RetryAfter / time.Second)
			if retry < 1 {
				retry = 1
			}
			c.Header("Retry-After", strconv.Itoa(retry))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded; try again in a moment",
			})
			return
		}
		c.Next()
	}
}
