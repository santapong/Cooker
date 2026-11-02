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
	providers := make([]string, 0, 2)
	if s.config.OIDC.Enabled && s.config.OIDC.Broker == "keycloak" && s.config.OIDC.IssuerURL != "" && s.config.OIDC.ClientID != "" {
		for _, provider := range s.config.OIDC.SocialProviders {
			if provider == "github" || provider == "google" {
				providers = append(providers, provider)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"oidc": gin.H{
			"enabled":         s.config.OIDC.Enabled,
			"broker":          s.config.OIDC.Broker,
			"socialProviders": providers,
		},
		"local": gin.H{
			"enabled":     s.config.LocalAuth.Enabled,
			"allowSignup": s.config.LocalAuth.Enabled && s.config.LocalAuth.AllowSignup,
		},
	})
}
