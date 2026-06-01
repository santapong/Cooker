package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResolveComposePath_Rejects(t *testing.T) {
	// Fix base so the test's expectations about relative-vs-absolute
	// stay stable across environments.
	prev := composeBaseDir
	t.Cleanup(func() { composeBaseDir = prev })
	composeBaseDir = t.TempDir()

	cases := []struct {
		name  string
		input string
	}{
		{"absolute path", "/etc/passwd"},
		{"relative traversal", "../etc/passwd"},
		{"nested traversal", "foo/../../etc/passwd"},
		{"subdir", "sub/compose.yml"},
		{"backslash separator", `..\etc\passwd`},
		{"hidden dotfile", ".env"},
		{"literal dot-dot", ".."},
		{"current dir", "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolveComposePath(tc.input); err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
		})
	}
}

func TestGetDockerImage_NotImplemented(t *testing.T) {
	r := gin.New()
	r.GET("/images/:id", GetDockerImage)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/images/abc", nil))
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"operation":"image.inspect"`) {
		t.Errorf("expected operation image.inspect, got %s", w.Body.String())
	}
}

func TestGetContainerLogs_NotImplemented(t *testing.T) {
	r := gin.New()
	r.GET("/containers/:id/logs", GetContainerLogs)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/containers/abc/logs", nil))
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"operation":"container.logs"`) {
		t.Errorf("expected operation container.logs, got %s", w.Body.String())
	}
}

func TestListDockerImages_EmptyOK(t *testing.T) {
	r := gin.New()
	r.GET("/images", ListDockerImages)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/images", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Errorf("expected [], got %q", got)
	}
}

func TestListContainers_EmptyOK(t *testing.T) {
	r := gin.New()
	r.GET("/containers", ListContainers)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/containers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Errorf("expected [], got %q", got)
	}
}

func TestResolveComposePath_Accepts(t *testing.T) {
	base := t.TempDir()
	prev := composeBaseDir
	t.Cleanup(func() { composeBaseDir = prev })
	composeBaseDir = base

	cases := []struct {
		name  string
		input string
	}{
		{"bare filename", "docker-compose.yml"},
		{"empty defaults to compose yml", ""},
		{"alt name", "compose.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveComposePath(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(got, base+string(filepath.Separator)) {
				t.Errorf("resolved path %q must stay inside base %q", got, base)
			}
		})
	}
}
