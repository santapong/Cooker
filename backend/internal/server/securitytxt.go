package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// securityTxtBody is the RFC 9116 "security.txt" disclosure document served at
// /.well-known/security.txt. It mirrors the reporting contact in SECURITY.md.
const securityTxtBody = `Contact: mailto:santapongsondhi@gmail.com
Expires: 2027-06-20T00:00:00Z
Preferred-Languages: en
Policy: https://github.com/santapong/Cooker/blob/main/SECURITY.md
`

// securityTxtHandler serves the public RFC 9116 security.txt document. It is
// registered on the bare router (not the authenticated /api/v1 group) so that
// security researchers can find the disclosure contact without credentials.
func securityTxtHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(securityTxtBody))
	}
}
