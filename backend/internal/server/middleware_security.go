package server

import "github.com/gin-gonic/gin"

// securityHeadersMiddleware sets a conservative set of security
// response headers on every API response. Values are picked to be
// safe defaults for an SPA-with-bearer-token API:
//
//   - X-Content-Type-Options: nosniff blocks MIME sniffing on browsers
//     that still respect it.
//   - X-Frame-Options: DENY because Cooker has no clickjack-relevant
//     embedding use case; the SPA owns its own page.
//   - Referrer-Policy: no-referrer keeps run / pipeline IDs out of
//     downstream Referer headers if a user clicks a log-line URL.
//   - Permissions-Policy disables hardware APIs we never use.
//   - Content-Security-Policy is intentionally strict: same-origin
//     scripts and styles only; data: images for inline favicons; the
//     SPA's own WebSocket connections fall under connect-src 'self'.
//     Frontends that legitimately need to extend this can add to it
//     via a future config knob.
//
// Cache-Control on secret-bearing responses is set per-route in
// the handler (RevealSecret etc.); a global default would suppress
// caching of /metrics too, which we don't want.
func securityHeadersMiddleware() gin.HandlerFunc {
	// Google Fonts is intentionally allow-listed — index.html links
	// stylesheets/fonts from there. Operators who self-host their
	// fonts can drop these via a future config knob.
	const csp = "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"img-src 'self' data:; " +
		"font-src 'self' data: https://fonts.gstatic.com; " +
		"connect-src 'self'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=()")
		h.Set("Content-Security-Policy", csp)
		c.Next()
	}
}