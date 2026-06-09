package server

import (
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// staticFixture writes an asset tree with one precompressed and one
// plain file and returns a router serving it.
func staticFixture(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app-abc123.js"), []byte("console.log('cooker')"), 0o644); err != nil {
		t.Fatal(err)
	}
	gz, err := os.Create(filepath.Join(dir, "app-abc123.js.gz"))
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(gz)
	if _, err := zw.Write([]byte("console.log('cooker')")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain-def456.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/assets/*filepath", assetsHandler(dir))
	return r, dir
}

func TestAssets_ImmutableCacheHeader(t *testing.T) {
	r, _ := staticFixture(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/plain-def456.css", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("no .gz sibling: Content-Encoding should be unset, got %q", got)
	}
}

func TestAssets_ServesPrecompressedGzip(t *testing.T) {
	r, _ := staticFixture(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := w.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("Content-Type = %q (must reflect .js, not .gz)", got)
	}
	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q", got)
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	defer zr.Close()
	buf := make([]byte, 64)
	n, _ := zr.Read(buf)
	if string(buf[:n]) != "console.log('cooker')" {
		t.Fatalf("decompressed body = %q", string(buf[:n]))
	}
}

func TestAssets_NoGzipWhenClientDoesNotAcceptIt(t *testing.T) {
	r, _ := staticFixture(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want unset", got)
	}
	if w.Body.String() != "console.log('cooker')" {
		t.Fatalf("body = %q", w.Body.String())
	}
}

// "gzip;q=0" is an explicit refusal, not an acceptance (RFC 9110).
func TestAssets_GzipQZeroIsRefusal(t *testing.T) {
	r, _ := staticFixture(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0, deflate")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want unset for q=0", got)
	}
	if w.Body.String() != "console.log('cooker')" {
		t.Fatalf("body = %q", w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0.5")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip for q=0.5", got)
	}
}

func TestAssets_MissingFile404WithoutImmutableHeader(t *testing.T) {
	r, _ := staticFixture(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/nope.js", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("a 404 must not carry Cache-Control, got %q", got)
	}
}

func TestAssets_TraversalIsContained(t *testing.T) {
	r, dir := staticFixture(t)
	// Plant a file *outside* the assets root that traversal would reach.
	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	// Encoded traversal: gin keeps the wildcard raw; path.Clean must
	// collapse it back inside the root.
	req := httptest.NewRequest(http.MethodGet, "/assets/../secret.txt", nil)
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK && w.Body.String() == "nope" {
		t.Fatal("path traversal escaped the assets root")
	}
}
