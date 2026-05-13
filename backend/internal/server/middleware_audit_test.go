package server

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/audit"
	"github.com/santapong/cooker/internal/auth"
)

// recordingSink captures emitted events without I/O.
type recordingSink struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recordingSink) Emit(e audit.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingSink) Close() error { return nil }

func (r *recordingSink) snapshot() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]audit.Event, len(r.events))
	copy(out, r.events)
	return out
}

func newAuditRouter(sink audit.Sink, claims *auth.Claims) *gin.Engine {
	r := gin.New()
	if claims != nil {
		r.Use(func(c *gin.Context) {
			c.Set("user", claims)
			c.Next()
		})
	}
	r.Use(auditMiddleware(sink))
	r.GET("/api/v1/things", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/api/v1/things", func(c *gin.Context) { c.Status(http.StatusCreated) })
	r.PUT("/api/v1/things/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.DELETE("/api/v1/things/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.PUT("/api/v1/environments/:id/secrets/:key", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func fire(r *gin.Engine, method, path string) int {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	r.ServeHTTP(w, req)
	return w.Code
}

func TestAudit_OnlyMutatingVerbs(t *testing.T) {
	sink := &recordingSink{}
	claims := &auth.Claims{Subject: "alice", Email: "alice@example.com"}
	r := newAuditRouter(sink, claims)

	fire(r, http.MethodGet, "/api/v1/things")
	fire(r, http.MethodPost, "/api/v1/things")
	fire(r, http.MethodPut, "/api/v1/things/42")
	fire(r, http.MethodDelete, "/api/v1/things/42")

	got := sink.snapshot()
	if len(got) != 3 {
		t.Fatalf("expected 3 audit events, got %d: %+v", len(got), got)
	}
	wantMethods := []string{http.MethodPost, http.MethodPut, http.MethodDelete}
	for i, e := range got {
		if e.Method != wantMethods[i] {
			t.Errorf("event %d: method %s, want %s", i, e.Method, wantMethods[i])
		}
		if e.UserSub != "alice" {
			t.Errorf("event %d: sub %q, want alice", i, e.UserSub)
		}
	}
}

func TestAudit_RouteTemplateNotConcretePath(t *testing.T) {
	sink := &recordingSink{}
	claims := &auth.Claims{Subject: "alice"}
	r := newAuditRouter(sink, claims)

	fire(r, http.MethodPut, "/api/v1/things/abc-123")
	got := sink.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Path != "/api/v1/things/:id" {
		t.Errorf("path = %q, want /api/v1/things/:id (template, not concrete)", got[0].Path)
	}
	if got[0].Status != http.StatusOK {
		t.Errorf("status = %d, want 200", got[0].Status)
	}
}

func TestAudit_NoClaimsNoEvent(t *testing.T) {
	sink := &recordingSink{}
	r := newAuditRouter(sink, nil) // no auth middleware -> no claims

	fire(r, http.MethodPost, "/api/v1/things")

	if got := sink.snapshot(); len(got) != 0 {
		t.Errorf("expected no events for unauthenticated request, got %d: %+v", len(got), got)
	}
}

func TestAudit_NilSinkIsNoop(t *testing.T) {
	r := gin.New()
	r.Use(auditMiddleware(nil))
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	if code := fire(r, http.MethodPost, "/x"); code != http.StatusOK {
		t.Errorf("nil sink should be a passthrough; got %d", code)
	}
}

func TestAudit_RedactedRoute_StillEmitsMetadata(t *testing.T) {
	// The middleware never captures the request body, so the secret
	// route is safe by construction. We *do* still want a record that
	// the secret-rotation happened — this test pins that contract.
	sink := &recordingSink{}
	claims := &auth.Claims{Subject: "alice"}
	r := newAuditRouter(sink, claims)

	fire(r, http.MethodPut, "/api/v1/environments/env1/secrets/DB_PASSWORD")
	got := sink.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Path != "/api/v1/environments/:id/secrets/:key" {
		t.Errorf("path = %q, want template form", got[0].Path)
	}
	// Belt-and-braces: the template list must include this path.
	if !audit.IsRedacted(got[0].Path) {
		t.Errorf("audit.IsRedacted should be true for %q", got[0].Path)
	}
}
