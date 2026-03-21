// Package auth provides SSO authentication via OpenID Connect (OIDC) with OAuth 2.0 PKCE flow.
// Supports identity providers: Keycloak, Okta, Azure AD, Google, GitHub.
package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/config"
)

// Claims represents the OIDC token claims extracted after validation.
type Claims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	Groups  []string `json:"groups"`
	Roles   []string `json:"roles"`
}

// OIDCMiddleware returns a Gin middleware that validates OIDC bearer tokens.
// When OIDC is disabled (dev mode), it sets a default user context.
func OIDCMiddleware(cfg config.OIDCConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.Enabled {
			// Dev mode: skip authentication, set default user
			c.Set("user", &Claims{
				Subject: "dev-user",
				Email:   "dev@cooker.local",
				Name:    "Developer",
				Groups:  []string{"admin"},
				Roles:   []string{string(RoleAdmin)},
			})
			c.Next()
			return
		}

		// Extract Bearer token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing Authorization header",
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid Authorization header format, expected: Bearer <token>",
			})
			return
		}

		token := parts[1]

		// TODO: Validate token using go-oidc provider
		// provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
		// verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
		// idToken, err := verifier.Verify(ctx, token)
		// idToken.Claims(&claims)

		// Placeholder: parse JWT claims (in production, use go-oidc verifier)
		claims, err := parseTokenClaims(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid token: " + err.Error(),
			})
			return
		}

		c.Set("user", claims)
		c.Next()
	}
}

// GetUser extracts the authenticated user from the Gin context.
func GetUser(c *gin.Context) *Claims {
	user, exists := c.Get("user")
	if !exists {
		return nil
	}
	claims, ok := user.(*Claims)
	if !ok {
		return nil
	}
	return claims
}

// parseTokenClaims is a placeholder for JWT parsing.
// In production, this is replaced by go-oidc token verification.
func parseTokenClaims(token string) (*Claims, error) {
	_ = token
	return &Claims{
		Subject: "placeholder",
		Email:   "user@example.com",
		Name:    "Placeholder User",
		Groups:  []string{"viewer"},
		Roles:   []string{string(RoleViewer)},
	}, nil
}
