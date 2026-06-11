package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/service"
)

// requireAPITokens aborts with 503 when the token service was not wired
// (defensive — server.New always sets it; nil means a store-less test
// Handler). Keeps the handler from panicking on nil.
func (h *Handler) requireAPITokens(c *gin.Context) bool {
	if h.APITokens != nil {
		return true
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error": "api token service unavailable",
	})
	return false
}

// callerIsToken reports whether the authenticated request is itself
// authenticated by an API token (Subject prefixed `token:`). Used to
// enforce the no-self-replication rule at the handler boundary as well as
// in the service (defence in depth).
func callerIsToken(claims *auth.Claims) bool {
	return claims != nil && strings.HasPrefix(claims.Subject, auth.TokenSubjectPrefix)
}

// createAPITokenRequest is the POST /tokens body. Identity (creator) is
// taken from the authenticated claims, never the body.
type createAPITokenRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
	// ExpiresAt is an optional RFC3339 timestamp. Omitted / null = never
	// expires (operators are advised to set a short expiry for CI tokens).
	ExpiresAt *time.Time `json:"expiresAt"`
}

// CreateAPIToken mints a personal-access / service-account token. The
// plaintext credential is returned exactly once in this response and is
// unrecoverable afterwards. The role is capped at the caller's own role;
// a request authenticated by a token may not create tokens. Product-plan
// Tier 1.
func (h *Handler) CreateAPIToken(c *gin.Context) {
	if !h.requireAPITokens(c) {
		return
	}
	claims := auth.GetUser(c)
	if claims == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	var req createAPITokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	tok, plaintext, err := h.APITokens.Create(c.Request.Context(), service.CreateParams{
		Name:          req.Name,
		Role:          req.Role,
		ExpiresAt:     req.ExpiresAt,
		CallerSub:     claims.Subject,
		CallerEmail:   claims.Email,
		CallerRoles:   claims.Roles,
		CallerIsToken: callerIsToken(claims),
	})
	if err != nil {
		h.abortAPITokenErr(c, err)
		return
	}

	// The plaintext is surfaced under `token` exactly once. The persisted
	// model (without the hash, which is json:"-") rides along so the
	// client can render the new row immediately.
	c.JSON(http.StatusCreated, gin.H{
		"token":    plaintext,
		"apiToken": tok,
	})
}

// ListAPITokens returns the caller's own tokens, or — for an admin passing
// ?all=true — every token. The hash is never serialised. Product-plan
// Tier 1.
func (h *Handler) ListAPITokens(c *gin.Context) {
	if !h.requireAPITokens(c) {
		return
	}
	claims := auth.GetUser(c)
	if claims == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	all := c.Query("all") == "true"

	tokens, err := h.APITokens.List(c.Request.Context(), claims.Subject, claims.Roles, all)
	if err != nil {
		h.abortAPITokenErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tokens": tokens})
}

// DeleteAPIToken revokes a token by id. Revocation is immediate (the row
// is deleted, so the next authenticated use fails). A non-admin may delete
// only their own tokens; a token-authenticated request may not delete any
// token. Product-plan Tier 1.
func (h *Handler) DeleteAPIToken(c *gin.Context) {
	if !h.requireAPITokens(c) {
		return
	}
	claims := auth.GetUser(c)
	if claims == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	err := h.APITokens.Delete(c.Request.Context(), service.DeleteParams{
		ID:            c.Param("id"),
		CallerSub:     claims.Subject,
		CallerRoles:   claims.Roles,
		CallerACR:     claims.ACR,
		CallerAMR:     claims.AMR,
		MFAValues:     h.MFAACRValues,
		CallerIsToken: callerIsToken(claims),
	})
	if err != nil {
		h.abortAPITokenErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("id")})
}

// abortAPITokenErr maps token-service sentinels to HTTP status codes.
// ErrNotOwner deliberately maps to 404 (not 403) so a caller can't probe
// the existence of another user's token id.
func (h *Handler) abortAPITokenErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTokenActorForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "token-authenticated requests cannot manage tokens"})
	case errors.Is(err, service.ErrMFARequired):
		// Same shape as auth.RequireMFA so the frontend re-issues the OIDC
		// redirect with acr_values and retries.
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa_required", "acr_values": h.MFAACRValues})
	case errors.Is(err, service.ErrRoleTooHigh):
		c.JSON(http.StatusForbidden, gin.H{"error": "requested role exceeds your own role"})
	case errors.Is(err, service.ErrInvalidRole):
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown role"})
	case errors.Is(err, service.ErrExpiryInPast):
		c.JSON(http.StatusBadRequest, gin.H{"error": "expiry is in the past"})
	case errors.Is(err, service.ErrAdminOnly):
		c.JSON(http.StatusForbidden, gin.H{"error": "admin role required"})
	case errors.Is(err, service.ErrNotOwner):
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
	default:
		if abortStoreErr(c, err, "token not found") {
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}
