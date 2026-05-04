package server

import (
	"bytes"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/idempotency"
)

// idempotencyTTL is how long a cached response stays available for
// replay. Picked to comfortably outlast GitHub's webhook retry budget
// and any reasonable client retry loop without bloating the cache.
const idempotencyTTL = 24 * time.Hour

// idempotencyMiddleware short-circuits duplicate mutating requests by
// caching the first successful response keyed by either the
// Idempotency-Key request header (RFC-draft) or, when present, the
// X-GitHub-Delivery header that GitHub stamps on every webhook
// delivery. A retry with the same key replays the cached status +
// body verbatim instead of spawning a duplicate run.
//
// Only 2xx responses are cached; failures are retried fresh. Empty
// keys pass through unchanged so legacy clients that don't send the
// header behave as before.
func idempotencyMiddleware(store idempotency.Store) gin.HandlerFunc {
	if store == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		key := c.GetHeader("Idempotency-Key")
		if key == "" {
			key = c.GetHeader("X-GitHub-Delivery")
		}
		if key == "" {
			c.Next()
			return
		}
		if cached, ok := store.Get(c.Request.Context(), key); ok {
			c.Header("Idempotency-Replayed", "true")
			c.Data(cached.Status, "application/json", cached.Body)
			c.Abort()
			return
		}
		rec := &captureWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
		c.Writer = rec
		c.Next()
		if rec.status >= 200 && rec.status < 300 && rec.body.Len() > 0 {
			_ = store.Set(c.Request.Context(), key, idempotency.Entry{
				Status: rec.status,
				Body:   append([]byte(nil), rec.body.Bytes()...),
			}, idempotencyTTL)
		}
	}
}

// captureWriter mirrors writes to both the underlying response writer
// and an in-memory buffer so the middleware can stash the body for
// replay. Default WriteHeader is 200 to match net/http semantics.
type captureWriter struct {
	gin.ResponseWriter
	status int
	body   *bytes.Buffer
}

func (w *captureWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}
