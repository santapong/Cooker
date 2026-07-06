package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSecurityTxt_ServesRFC9116(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/.well-known/security.txt", securityTxtHandler())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want to contain text/plain", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Contact") {
		t.Fatalf("body missing Contact field:\n%s", body)
	}
	if !strings.Contains(body, "Expires") {
		t.Fatalf("body missing Expires field:\n%s", body)
	}

	// Expires must be a valid RFC3339 timestamp in the future (RFC 9116 §2.5.5).
	var expires string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "Expires:") {
			expires = strings.TrimSpace(strings.TrimPrefix(line, "Expires:"))
			break
		}
	}
	if expires == "" {
		t.Fatal("could not extract Expires value from body")
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		t.Fatalf("Expires = %q does not parse as RFC3339: %v", expires, err)
	}
	if !exp.After(time.Now()) {
		t.Fatalf("Expires = %q is not in the future", expires)
	}
}
