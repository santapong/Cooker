package server

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// assetsHandler serves the Vite build output under /assets with the
// caching headers router.Static never set (P26-05-33):
//
//   - Vite emits content-hashed filenames, so every asset is immutable:
//     Cache-Control: public, max-age=31536000, immutable. Repeat visits
//     skip the download entirely.
//   - When the client accepts gzip and a precompressed sibling
//     (<file>.gz, written by the Dockerfile build) exists, serve it with
//     Content-Encoding: gzip and the original file's Content-Type —
//     ~70% less JS/CSS over the wire without per-request compression.
//
// dir is the on-disk assets root (e.g. /usr/share/cooker/static/assets).
func assetsHandler(dir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Route param is "/<file>"; Clean collapses any ".." so the
		// joined path cannot escape dir.
		rel := path.Clean("/" + c.Param("filepath"))
		full := filepath.Join(dir, filepath.FromSlash(rel))

		st, err := os.Stat(full)
		if err != nil || st.IsDir() {
			// No caching headers on the miss: a 404 cached as immutable
			// would pin the error in the browser for a year.
			c.Status(http.StatusNotFound)
			return
		}

		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Header("Vary", "Accept-Encoding")

		if acceptsGzip(c.Request) {
			if gzInfo, err := os.Stat(full + ".gz"); err == nil && !gzInfo.IsDir() {
				// Content-Type must come from the *original* extension;
				// http.ServeFile would otherwise infer application/gzip.
				if ctype := contentTypeByExt(rel); ctype != "" {
					c.Header("Content-Type", ctype)
					c.Header("Content-Encoding", "gzip")
					http.ServeFile(c.Writer, c.Request, full+".gz")
					return
				}
			}
		}
		http.ServeFile(c.Writer, c.Request, full)
	}
}

// spaIndexHandler serves index.html for client-side routes. The entry
// point references hashed assets, so it must revalidate on each load
// (no-cache = revalidate, not "don't cache"): a stale index would point
// at assets that no longer exist after a deploy.
func spaIndexHandler(indexPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.File(indexPath)
	}
}

func acceptsGzip(r *http.Request) bool {
	for _, enc := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		e := strings.TrimSpace(enc)
		if e == "gzip" || strings.HasPrefix(e, "gzip;") {
			return true
		}
	}
	return false
}

// contentTypeByExt maps the small set of extensions Vite emits to their
// MIME types. Returning "" makes the caller fall back to the
// uncompressed file (and net/http's own sniffing) rather than guess.
func contentTypeByExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	default:
		return ""
	}
}
