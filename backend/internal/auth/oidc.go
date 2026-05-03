// Package auth provides SSO authentication via OpenID Connect (OIDC) with OAuth 2.0 PKCE flow.
// Supports identity providers: Keycloak, Okta, Azure AD, Google, GitHub.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/auth/local"
	"github.com/cooker-ci/cooker/internal/config"
)

// providerDiscoveryRetry is the cool-down between failed attempts to
// discover the OIDC provider. Keeps boot fast and the IdP unhammered.
const providerDiscoveryRetry = 30 * time.Second

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

// Middleware validates bearer tokens on incoming requests. It can hold
// up to two verifiers — one for OIDC IdP-issued tokens and one for
// JWTs Cooker itself issued via the local-auth path. The local
// verifier is consulted first when a token's `iss` claim matches
// cooker-local; otherwise OIDC takes over.
//
// When both OIDC and local auth are disabled (dev mode) the
// middleware injects a deterministic admin user.
type Middleware struct {
	cfg         config.OIDCConfig
	localIssuer *local.Issuer

	// mu guards verifier, nextProviderTry, and lastJWKSRefresh.
	// OIDC provider discovery is deferred to first request so that
	// boot survives a transient IdP outage. nextProviderTry caps the
	// retry rate; lastJWKSRefresh feeds the readiness probe.
	mu              sync.Mutex
	verifier        *oidc.IDTokenVerifier
	nextProviderTry time.Time
	lastJWKSRefresh time.Time
}

// NewMiddleware builds an auth middleware. Returns an error only when
// configuration itself is invalid (missing required fields, bad local
// signing key). Provider discovery is deferred to the first
// authenticated request so a temporarily unreachable IdP doesn't
// prevent Cooker from starting.
func NewMiddleware(_ context.Context, cfg config.OIDCConfig) (*Middleware, error) {
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
	return m, nil
}

// ensureProvider lazily discovers the OIDC provider on the first
// authenticated request. Subsequent failures are rate-limited to one
// attempt every providerDiscoveryRetry seconds so Cooker doesn't
// hammer a recovering IdP.
func (m *Middleware) ensureProvider(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.verifier != nil {
		return nil
	}
	if !m.nextProviderTry.IsZero() && time.Now().Before(m.nextProviderTry) {
		return errors.New("provider discovery cooling down")
	}
	provider, err := oidc.NewProvider(ctx, m.cfg.IssuerURL)
	if err != nil {
		m.nextProviderTry = time.Now().Add(providerDiscoveryRetry)
		return fmt.Errorf("discover provider %q: %w", m.cfg.IssuerURL, err)
	}
	m.verifier = provider.Verifier(&oidc.Config{ClientID: m.cfg.ClientID})
	m.nextProviderTry = time.Time{}
	return nil
}

// recordJWKSRefresh stamps the time of the most recent successful
// token verification so the readiness probe can report JWKS freshness.
func (m *Middleware) recordJWKSRefresh() {
	m.mu.Lock()
	m.lastJWKSRefresh = time.Now()
	m.mu.Unlock()
}

// LastJWKSRefresh returns how long ago the JWKS was last successfully
// used to verify a token. The bool is false until the first verify
// has happened (boot, no traffic). Used by the /health/ready handler.
func (m *Middleware) LastJWKSRefresh() (time.Duration, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastJWKSRefresh.IsZero() {
		return 0, false
	}
	return time.Since(m.lastJWKSRefresh), true
}

// EnableLocalAuth attaches the local-auth issuer so the middleware
// will accept Cooker-issued JWTs. Pass a nil issuer to disable.
func (m *Middleware) EnableLocalAuth(issuer *local.Issuer) {
	m.localIssuer = issuer
}

// Handler returns a Gin middleware that validates bearer tokens.
func (m *Middleware) Handler() gin.HandlerFunc {
	if !m.cfg.Enabled && m.localIssuer == nil {
		return devHandler()
	}
	return func(c *gin.Context) {
		token, ok := bearerToken(c)
		if !ok {
			return
		}
		// Local-auth fast path: probe the token's iss before paying
		// for an OIDC verify. Local tokens never go through the OIDC
		// IDTokenVerifier (their iss is "cooker-local", not the IdP).
		if m.localIssuer != nil && local.LooksLikeLocalToken(token) {
			claims, err := m.localIssuer.Verify(token)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "invalid token: " + err.Error(),
				})
				return
			}
			c.Set("user", &Claims{
				Subject: claims.Subject,
				Email:   claims.Email,
				Name:    claims.Name,
				Groups:  []string{},
				Roles:   []string{claims.Role},
				// Local auth never satisfies MFA; an operator who
				// requires MFA on destructive routes will still need
				// to point those users at OIDC. Documented in
				// SECURITY.md.
			})
			c.Next()
			return
		}
		if !m.cfg.Enabled {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "OIDC disabled and token is not a local-auth token",
			})
			return
		}
		if err := m.ensureProvider(c.Request.Context()); err != nil {
			c.Header("Retry-After", "30")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "oidc: " + err.Error(),
			})
			return
		}
		idToken, err := m.verifier.Verify(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid token: " + err.Error(),
			})
			return
		}
		m.recordJWKSRefresh()
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
