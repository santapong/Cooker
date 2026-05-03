package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

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

		// Run DB and Redis probes in parallel against the shared 1s
		// deadline so worst-case probe latency is max(db, redis), not
		// their sum. errgroup is used purely for parallel run + Wait;
		// the goroutines never return an error because we want both
		// probe results, not first-error semantics.
		var dbResult, redisResult string
		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() error {
			if err := st.Ping(gctx); err != nil {
				dbResult = "err: " + err.Error()
			} else {
				dbResult = "ok"
			}
			return nil
		})
		if redisClient != nil {
			g.Go(func() error {
				if err := redisClient.Ping(gctx).Err(); err != nil {
					redisResult = "err: " + err.Error()
				} else {
					redisResult = "ok"
				}
				return nil
			})
		}
		_ = g.Wait()

		if dbResult == "ok" {
			checks["db"] = "ok"
		} else {
			checks["db"] = dbResult
			ok = false
		}
		if redisClient != nil {
			if redisResult == "ok" {
				checks["redis"] = "ok"
			} else {
				checks["redis"] = redisResult
				ok = false
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
