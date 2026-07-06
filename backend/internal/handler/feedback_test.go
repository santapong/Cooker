package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/service"
)

func feedbackRouter(h *Handler) *gin.Engine {
	op := &auth.Claims{Subject: "user-123", Email: "op@example.com", Roles: []string{string(auth.RoleViewer)}}
	r := gin.New()
	r.POST("/feedback", withUser(h.SubmitFeedback, op))
	r.GET("/capabilities", withUser(h.GetCapabilities, op))
	return r
}

func TestSubmitFeedback_Disabled503AndCapabilityOff(t *testing.T) {
	h := newTestHandler(t)
	r := feedbackRouter(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/feedback", map[string]string{"category": "bug", "message": "hi"}))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
	if !strings.Contains(w.Body.String(), `"feedback":false`) {
		t.Errorf("capabilities = %s", w.Body.String())
	}
}

func TestSubmitFeedback_Validation400(t *testing.T) {
	h := newTestHandler(t)
	h.Feedback = service.NewFeedbackService("owner/repo", "test-token")
	r := feedbackRouter(h)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"invalid category", map[string]string{"category": "rant", "message": "hi"}},
		{"empty message", map[string]string{"category": "bug", "message": ""}},
		{"whitespace-only message", map[string]string{"category": "bug", "message": "  \n\t "}},
		{"message too long", map[string]string{"category": "bug", "message": strings.Repeat("a", 4001)}},
		{"pageUrl too long", map[string]string{"category": "bug", "message": "hi", "pageUrl": strings.Repeat("u", 501)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, newRequest(http.MethodPost, "/feedback", tt.body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSubmitFeedback_Happy201(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"html_url":"https://github.com/owner/repo/issues/7","number":7}`))
	}))
	defer srv.Close()

	h := newTestHandler(t)
	fs := service.NewFeedbackService("owner/repo", "test-token")
	fs.APIBase = srv.URL
	h.Feedback = fs

	w := httptest.NewRecorder()
	feedbackRouter(h).ServeHTTP(w, newRequest(http.MethodPost, "/feedback",
		map[string]string{"category": "idea", "message": "dark mode", "pageUrl": "https://cooker.example/apps"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("code = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"issueUrl":"https://github.com/owner/repo/issues/7"`) ||
		!strings.Contains(w.Body.String(), `"issueNumber":7`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

// GitHub failure text can carry its response body — the client must
// only ever see the generic message.
func TestSubmitFeedback_Upstream500Is502Generic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"secret-diagnostic-detail"}`))
	}))
	defer srv.Close()

	h := newTestHandler(t)
	fs := service.NewFeedbackService("owner/repo", "test-token")
	fs.APIBase = srv.URL
	h.Feedback = fs

	w := httptest.NewRecorder()
	feedbackRouter(h).ServeHTTP(w, newRequest(http.MethodPost, "/feedback",
		map[string]string{"category": "other", "message": "hello"}))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret-diagnostic-detail") {
		t.Fatalf("upstream error text relayed to client: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to file feedback") {
		t.Errorf("body = %s", w.Body.String())
	}
}
