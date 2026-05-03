package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// authMethods reports which auth paths the server has enabled. The
// frontend uses this to decide whether to render the SSO button, the
// email/password form, or both. Returned without authentication so
// the sign-in page can call it before the user has a session.
func (s *Server) authMethods(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"oidc": gin.H{
			"enabled": s.config.OIDC.Enabled,
		},
		"local": gin.H{
			"enabled":     s.config.LocalAuth.Enabled,
			"allowSignup": s.config.LocalAuth.Enabled && s.config.LocalAuth.AllowSignup,
		},
	})
}
