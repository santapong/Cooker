package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// buildSecurityTxtBody assembles the RFC 9116 "security.txt" disclosure document
// served at /.well-known/security.txt. It mirrors the reporting contact in
// SECURITY.md. The Expires field is computed dynamically (one year out) so the
// document never lapses into RFC 9116-invalid state (§2.5.5).
func buildSecurityTxtBody() []byte {
	expires := time.Now().UTC().AddDate(1, 0, 0).Format(time.RFC3339)
	return []byte(fmt.Sprintf(`Contact: mailto:santapongsondhi@gmail.com
Expires: %s
Preferred-Languages: en
Policy: https://github.com/santapong/Cooker/blob/main/SECURITY.md
`, expires))
}

// securityTxtHandler serves the public RFC 9116 security.txt document. It is
// registered on the bare router (not the authenticated /api/v1 group) so that
// security researchers can find the disclosure contact without credentials.
//
// The body (including the dynamic Expires timestamp) is computed once when the
// handler is constructed at startup.
func securityTxtHandler() gin.HandlerFunc {
	body := buildSecurityTxtBody()
	return func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain; charset=utf-8", body)
	}
}
