package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// dockerWriteCase drives a single stubbed docker write endpoint and
// asserts it now returns an honest 501 with the expected operation tag
// instead of the old fake 2xx (HS26-05-15). A valid body is sent so the
// handler reaches the 501 rather than short-circuiting on a bind 400.
type dockerWriteCase struct {
	name   string
	method string
	route  string
	path   string
	body   string
	op     string
	handle gin.HandlerFunc
}

func TestDockerWriteEndpoints_NotImplemented(t *testing.T) {
	cases := []dockerWriteCase{
		{"build image", http.MethodPost, "/images/build", "/images/build", `{"dockerfile":"FROM scratch","tags":["a:b"]}`, "image.build", BuildDockerImage},
		{"delete image", http.MethodDelete, "/images/:id", "/images/sha256-abc", "", "image.remove", DeleteDockerImage},
		{"create container", http.MethodPost, "/containers", "/containers", `{"image":"nginx:1.25"}`, "container.create", CreateContainer},
		{"stop container", http.MethodPost, "/containers/:id/stop", "/containers/abc/stop", "", "container.stop", StopContainer},
		{"delete container", http.MethodDelete, "/containers/:id", "/containers/abc", "", "container.remove", DeleteContainer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Handle(tc.method, tc.route, tc.handle)
			var bodyReader *strings.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			} else {
				bodyReader = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, bodyReader)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusNotImplemented {
				t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), `"operation":"`+tc.op+`"`) {
				t.Errorf("expected operation %q, got %s", tc.op, w.Body.String())
			}
			// No fake-success leakage from the old stubs.
			for _, leak := range []string{"build-placeholder", "container-placeholder", `"message":"stopped"`, `"message":"removed"`, `"message":"image deleted"`} {
				if strings.Contains(w.Body.String(), leak) {
					t.Errorf("response still leaks fake-success token %q: %s", leak, w.Body.String())
				}
			}
		})
	}
}

// TestBuildDockerImage_BadBodyStill400 confirms the bind-error path still
// 400s ahead of the 501 — malformed input is a client error, not a
// feature gap.
func TestBuildDockerImage_BadBodyStill400(t *testing.T) {
	r := gin.New()
	r.POST("/images/build", BuildDockerImage)
	req := httptest.NewRequest(http.MethodPost, "/images/build", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d: %s", w.Code, w.Body.String())
	}
}

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
