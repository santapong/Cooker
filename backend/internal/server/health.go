package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/cooker-ci/cooker/internal/store"
)

const readinessProbeTimeout = 1 * time.Second

// livenessHandler reports whether the process is up. The Gin router
// being able to serve this route is itself the answer.
func livenessHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "cooker"})
	}
}

// readinessHandler reports whether dependencies the request path needs
// (database, Redis if configured, JWKS if OIDC enabled) are reachable.
// Returns 503 with a per-check breakdown on any failure so operators
// can tell which dependency tripped the probe.
func readinessHandler(st *store.Store, redisClient *redis.Client, jwksAge func() (time.Duration, bool)) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessProbeTimeout)
		defer cancel()
		checks := gin.H{}
		ok := true

		if err := st.Ping(ctx); err != nil {
			checks["db"] = "err: " + err.Error()
			ok = false
		} else {
			checks["db"] = "ok"
		}

		if redisClient != nil {
			if err := redisClient.Ping(ctx).Err(); err != nil {
				checks["redis"] = "err: " + err.Error()
				ok = false
			} else {
				checks["redis"] = "ok"
			}
		} else {
			checks["redis"] = "n/a"
		}

		if jwksAge != nil {
			if age, valid := jwksAge(); valid {
				checks["jwks_age_s"] = int(age.Seconds())
			} else {
				checks["jwks_age_s"] = "n/a"
			}
		} else {
			checks["jwks_age_s"] = "n/a"
		}

		status := http.StatusOK
		body := gin.H{"status": "ok", "checks": checks}
		if !ok {
			status = http.StatusServiceUnavailable
			body["status"] = "unavailable"
		}
		c.JSON(status, body)
	}
}
