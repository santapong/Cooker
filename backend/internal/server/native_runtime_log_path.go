package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/service"
)

const nativeRuntimeLogPrefix = "/ws/pipelines/"

func nativeRuntimeLogID(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "/\\?#") && !strings.ContainsFunc(value, unicode.IsControl)
}

// This parser consumes an already decoded path. In particular, a literal
// percent sequence in an ID must never be unescaped for a second time.
func nativeRuntimeLogSubject(path string) (service.NativeRuntimeSubject, bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 9 || parts[0] != "" || parts[1] != "ws" || parts[2] != "pipelines" || parts[4] != "runs" || parts[6] != "stages" || parts[8] != "runtime-logs" ||
		!nativeRuntimeLogID(parts[3]) || !nativeRuntimeLogID(parts[5]) || !nativeRuntimeLogID(parts[7]) {
		return service.NativeRuntimeSubject{}, false
	}
	return service.NativeRuntimeSubject{Kind: "native-stage", PipelineID: parts[3], RunID: parts[5], StageID: parts[7]}, true
}

// Only the native route uses this once-only decoding contract. Existing App,
// build and stage log ticket paths retain their previous normalization.
func canonicalNativeRuntimeLogPath(raw string) (string, bool) {
	if !strings.HasPrefix(raw, nativeRuntimeLogPrefix) {
		return "", false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", false
	}
	_, ok := nativeRuntimeLogSubject(decoded)
	return decoded, ok
}

func nativeRuntimeLogError(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrRuntimeLogRequest):
		return http.StatusBadRequest, service.ErrRuntimeLogRequest.Error()
	case errors.Is(err, service.ErrRuntimeLogExpired):
		return http.StatusNotFound, service.ErrRuntimeLogExpired.Error()
	case errors.Is(err, service.ErrRuntimeLogCapacity):
		return http.StatusTooManyRequests, service.ErrRuntimeLogCapacity.Error()
	default:
		return http.StatusServiceUnavailable, service.ErrRuntimeLogUnavailable.Error()
	}
}

// Run before authentication/role gates for native log paths only, so denied
// responses have the same privacy headers as successful snapshots.
func nativeRuntimeLogHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Split(c.Request.URL.Path, "/")
		native := strings.HasPrefix(c.Request.URL.Path, nativeRuntimeLogPrefix)
		if len(parts) >= 10 && parts[0] == "" && parts[1] == "api" && parts[2] == "v1" && parts[3] == "pipelines" && parts[5] == "runs" && parts[7] == "stages" && (parts[9] == "log-replicas" || parts[9] == "log-sessions") {
			native = true
		}
		if native {
			c.Header("Cache-Control", "no-store")
			c.Header("X-Content-Type-Options", "nosniff")
		}
		c.Next()
	}
}
