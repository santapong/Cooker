// Package auth provides SSO authentication via OpenID Connect (OIDC) with OAuth 2.0 PKCE flow.
// Supports identity providers: Keycloak, Okta, Azure AD, Google, GitHub.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
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
	// ACR is the Authentication Context Class Reference. RequireMFA
	// matches against this to decide if step-up auth has happened.
	ACR string `json:"acr"`
	// AMR is the Authentication Methods References (e.g. ["pwd","otp"]).
	// RequireMFA accepts a token whose AMR contains a configured value
	// even when ACR is empty.
	AMR []string `json:"amr"`
}

// Middleware validates OIDC bearer tokens on incoming requests.
//
// When OIDC is disabled (dev mode) it injects a deterministic admin
// user. When enabled it discovers the provider at construction time so
// Cooker fails fast on misconfiguration, and uses the resulting
// IDTokenVerifier to check `iss`, `aud`, `exp`, and signature on every
// request.
type Middleware struct {
	cfg      config.OIDCConfig
	verifier *oidc.IDTokenVerifier
}

// NewMiddleware builds an OIDC middleware. Returns an error when
// OIDC is enabled but the issuer is unreachable or required fields are
// missing.
func NewMiddleware(ctx context.Context, cfg config.OIDCConfig) (*Middleware, error) {
	m := &Middleware{cfg: cfg}
	if !cfg.Enabled {
		return m, nil
	}
	if cfg.IssuerURL == "" {
		return nil, fmt.Errorf("oidc enabled but COOKER_OIDC_ISSUER_URL is empty")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("oidc enabled but COOKER_OIDC_CLIENT_ID is empty")
	}
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc: discover provider %q: %w", cfg.IssuerURL, err)
	}
	m.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	return m, nil
}

// Handler returns a Gin middleware that validates OIDC bearer tokens.
func (m *Middleware) Handler() gin.HandlerFunc {
	if !m.cfg.Enabled {
		return devHandler()
	}
	return func(c *gin.Context) {
		token, ok := bearerToken(c)
		if !ok {
			return
		}
		idToken, err := m.verifier.Verify(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid token: " + err.Error(),
			})
			return
		}
		var raw struct {
			Subject string   `json:"sub"`
			Email   string   `json:"email"`
			Name    string   `json:"name"`
			Groups  []string `json:"groups"`
			ACR     string   `json:"acr"`
			AMR     []string `json:"amr"`
		}
		if err := idToken.Claims(&raw); err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "cannot parse token claims: " + err.Error(),
			})
			return
		}
		claims := &Claims{
			Subject: raw.Subject,
			Email:   raw.Email,
			Name:    raw.Name,
			Groups:  raw.Groups,
			Roles:   MapGroupsToRolesWith(raw.Groups, m.cfg.GroupRoleMap),
			ACR:     raw.ACR,
			AMR:     raw.AMR,
		}
		c.Set("user", claims)
		c.Next()
	}
}

// devHandler injects a default admin user when OIDC is disabled.
func devHandler() gin.HandlerFunc {
	devUser := &Claims{
		Subject: "dev-user",
		Email:   "dev@cooker.local",
		Name:    "Developer",
		Groups:  []string{"cooker-admins"},
		Roles:   []string{string(RoleAdmin)},
	}
	return func(c *gin.Context) {
		c.Set("user", devUser)
		c.Next()
	}
}

// bearerToken pulls the bearer token out of the Authorization header.
// Aborts the request with 401 if missing or malformed and returns ok=false.
func bearerToken(c *gin.Context) (string, bool) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "missing Authorization header",
		})
		return "", false
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || strings.TrimSpace(parts[1]) == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "invalid Authorization header format, expected: Bearer <token>",
		})
		return "", false
	}
	return strings.TrimSpace(parts[1]), true
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
