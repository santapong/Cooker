package server

import (
	"net/http"
	"runtime"

	"github.com/gin-gonic/gin"
)

// Build-time version metadata. Populate via ldflags, e.g.:
//
//	go build -ldflags "-X github.com/cooker-ci/cooker/internal/server.BuildSHA=$(git rev-parse --short HEAD) \
//	                   -X github.com/cooker-ci/cooker/internal/server.BuildVersion=$(git describe --tags --always) \
//	                   -X github.com/cooker-ci/cooker/internal/server.BuildTime=$(date -u +%FT%TZ)"
//
// Defaults are "dev" so the endpoint still responds in test
// builds; ops dashboards and grafana panels look at this to
// correlate logs/metrics with the live binary.
var (
	BuildSHA     = "dev"
	BuildVersion = "dev"
	BuildTime    = "dev"
)

// versionHandler returns the build metadata as JSON. Unauthenticated
// — exposing the build SHA is intentional (it's how operators
// confirm a rollout reached every replica).
func versionHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"build_sha":     BuildSHA,
			"build_version": BuildVersion,
			"build_time":    BuildTime,
			"go_version":    runtime.Version(),
		})
	}
}