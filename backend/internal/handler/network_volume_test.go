package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func newTestRouter(register func(r *gin.Engine, h *Handler)) (*gin.Engine, *Handler) {
	r := gin.New()
	h := &Handler{}
	register(r, h)
	return r, h
}

func TestListDockerNetworks_EmptyOK(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/networks", h.ListDockerNetworks)
	})
	_ = h
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/networks", nil))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Errorf("expected [], got %q", got)
	}
}

func TestCreateDockerNetwork_NotImplemented(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.POST("/networks", h.CreateDockerNetwork)
	})
	_ = h
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/networks", bytes.NewReader([]byte(`{"name":"n1"}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "network.create") {
		t.Errorf("expected operation in body, got %s", w.Body.String())
	}
}

func TestCreateDockerVolume_NotImplemented(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.POST("/volumes", h.CreateDockerVolume)
	})
	_ = h
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/volumes", bytes.NewReader([]byte(`{"name":"v1"}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "volume.create") {
		t.Errorf("expected operation in body, got %s", w.Body.String())
	}
}

func TestDeleteDockerNetwork_NotImplemented(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.DELETE("/networks/:id", h.DeleteDockerNetwork)
	})
	_ = h
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/networks/abc", nil))
	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d", w.Code)
	}
}
