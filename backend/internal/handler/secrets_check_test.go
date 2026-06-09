package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

func secretsTestRouter(h *Handler) *gin.Engine {
	r := gin.New()
	r.POST("/settings/secrets/test", h.TestSecretsBackend)
	return r
}

func TestTestSecretsBackend_NoBackend(t *testing.T) {
	h := newTestHandler(t)
	h.Secrets = nil
	w := httptest.NewRecorder()
	secretsTestRouter(h).ServeHTTP(w, newRequest(http.MethodPost, "/settings/secrets/test", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestTestSecretsBackend_NoEnvironments(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	secretsTestRouter(h).ServeHTTP(w, newRequest(http.MethodPost, "/settings/secrets/test", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409: %s", w.Code, w.Body.String())
	}
}

func TestTestSecretsBackend_Happy(t *testing.T) {
	h := newTestHandler(t)
	h.SecretsBackend = "database"
	env := &model.Environment{ID: "env-1", Name: "dev"}
	if err := h.Store.Environments.Create(newRequest(http.MethodPost, "/", nil).Context(), env); err != nil {
		t.Fatal(err)
	}

	// Default env (empty body) and explicit environmentId both probe.
	for _, body := range []any{nil, map[string]string{"environmentId": "env-1"}} {
		w := httptest.NewRecorder()
		secretsTestRouter(h).ServeHTTP(w, newRequest(http.MethodPost, "/settings/secrets/test", body))
		if w.Code != http.StatusOK {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		var res service.SecretsCheckResult
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if !res.OK || res.Backend != "database" || res.Error != "" {
			t.Fatalf("unexpected result: %+v", res)
		}
	}
}
