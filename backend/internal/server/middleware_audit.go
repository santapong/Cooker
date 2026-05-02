package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/audit"
	"github.com/cooker-ci/cooker/internal/auth"
)

// auditMiddleware emits one audit event per mutating, authenticated
// request. Read-only verbs (GET, HEAD, OPTIONS) are skipped: they
// don't change state and would drown the trail in noise. Unauthenticated
// requests (no claims in context) are also skipped — the only routes
// that hit this middleware are under /api/v1, which already requires
// a session, but be defensive in case the chain is rewired later.
//
// The middleware never reads request or response bodies, so secret-
// bearing endpoints (audit.IsRedacted) are safe by construction. If a
// future change adds body capture, it must consult audit.IsRedacted
// first.
func auditMiddleware(sink audit.Sink) gin.HandlerFunc {
	if sink == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		if !isMutating(c.Request.Method) {
			return
		}
		claims := auth.GetUser(c)
		if claims == nil {
			return
		}
		sink.Emit(audit.Event{
			Time:     start.UTC(),
			UserSub:  claims.Subject,
			UserMail: claims.Email,
			Method:   c.Request.Method,
			Path:     c.FullPath(),
			Status:   c.Writer.Status(),
			Latency:  time.Since(start).String(),
			ClientIP: c.ClientIP(),
		})
	}
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}
