package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestLivenessHandler_AlwaysOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health/live", livenessHandler())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestReadinessHandler_DBPingErrorReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := store.New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, func(ctx context.Context) error {
		return errors.New("connection refused")
	})
	r := gin.New()
	r.GET("/health/ready", readinessHandler(st, nil, nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	checks, _ := body["checks"].(map[string]any)
	if checks == nil || checks["db"] == "ok" {
		t.Fatalf("expected db check to fail, body=%v", body)
	}
}

func TestReadinessHandler_AllOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := memory.New() // in-memory has nil ping → reports ok
	r := gin.New()
	r.GET("/health/ready", readinessHandler(st, nil, func() (time.Duration, bool) {
		return 30 * time.Second, true
	}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	checks, _ := body["checks"].(map[string]any)
	if checks == nil || checks["db"] != "ok" || checks["redis"] != "n/a" {
		t.Fatalf("unexpected checks: %v", checks)
	}
}
