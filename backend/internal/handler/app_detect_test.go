package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/github"
)

func detectRouter(h *Handler) *gin.Engine {
	op := &auth.Claims{Email: "op@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/apps/detect-build", withUser(h.DetectAppBuild, op))
	return r
}

func TestDetectAppBuild_NilDetector503(t *testing.T) {
	h := newTestHandler(t)
	r := detectRouter(h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps/detect-build",
		map[string]string{"githubRepo": "acme/web", "branch": "main"}))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503: %s", w.Code, w.Body.String())
	}
}

func TestDetectAppBuild_InvalidRepo400(t *testing.T) {
	h := newTestHandler(t)
	h.AppDetector = service.NewAppDetector()
	r := detectRouter(h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps/detect-build",
		map[string]string{"githubRepo": "not a repo", "branch": "main"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestDetectAppBuild_CloneFailure422(t *testing.T) {
	h := newTestHandler(t)
	h.AppDetector = service.NewAppDetectorWithClone(
		func(_ context.Context, _ github.CloneOptions) (string, error) {
			return "", errors.New("repository not found")
		})
	r := detectRouter(h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps/detect-build",
		map[string]string{"githubRepo": "acme/missing", "branch": "main"}))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422: %s", w.Code, w.Body.String())
	}
}

func TestDetectAppBuild_Happy(t *testing.T) {
	h := newTestHandler(t)
	h.AppDetector = service.NewAppDetectorWithClone(
		func(_ context.Context, _ github.CloneOptions) (string, error) {
			dir, err := os.MkdirTemp("", "detect-handler-*")
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644); err != nil {
				return "", err
			}
			return dir, nil
		})
	r := detectRouter(h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps/detect-build",
		map[string]string{"githubRepo": "acme/web", "branch": "main"}))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"suggestedRecipe":"go"`, `"plan"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %s: %s", want, body)
		}
	}
}
