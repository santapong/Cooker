package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// LocalAuthHandler exposes the email + password endpoints. Wired into
// the router only when COOKER_LOCAL_AUTH_ENABLED=true.
type LocalAuthHandler struct {
	users       store.UserStore
	issuer      *local.Issuer
	allowSignup bool
}

// NewLocalAuthHandler constructs the handler. Pass allowSignup=false
// to disable POST /signup (admin-only invite flow).
func NewLocalAuthHandler(users store.UserStore, issuer *local.Issuer, allowSignup bool) *LocalAuthHandler {
	return &LocalAuthHandler{users: users, issuer: issuer, allowSignup: allowSignup}
}

type signupRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	Name     string `json:"name"`
}

type signinRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type tokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	User      meUser    `json:"user"`
}

type meUser struct {
	ID    string   `json:"id"`
	Email string   `json:"email"`
	Name  string   `json:"name"`
	Role  string   `json:"role"`
	Roles []string `json:"roles"`
}

// Signup creates a new local-auth account. The very first signup is
// promoted to admin (bootstrap pattern); subsequent signups default
// to viewer and must be promoted by an admin.
//
// Errors deliberately don't reveal whether an email exists pre-create
// — the only signal is the 409 on duplicate, which we accept because
// account-existence is something an attacker could probe via the
// signin endpoint anyway. Cooker leans on rate-limiting (per-user on
// authenticated routes) for brute-force protection.
func (h *LocalAuthHandler) Signup(c *gin.Context) {
	if !h.allowSignup {
		c.JSON(http.StatusForbidden, gin.H{"error": "signup is disabled; ask an admin to invite you"})
		return
	}
	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	email, err := local.NormaliseEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	hash, err := local.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	if existing, err := h.users.GetByEmail(ctx, email); err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "an account with that email already exists"})
		return
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		slog.Error("auth/local: lookup before create failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	role, err := local.BootstrapRole(ctx, h.users)
	if err != nil {
		slog.Error("auth/local: bootstrap role", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	now := time.Now().UTC()
	u := &model.User{
		ID:           uuid.NewString(),
		Email:        email,
		Name:         strings.TrimSpace(req.Name),
		PasswordHash: hash,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := h.users.Create(ctx, u); err != nil {
		slog.Error("auth/local: create user", "error", err, "email", email)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create account"})
		return
	}
	slog.Info("auth/local: account created", "email", email, "role", role)

	tok, exp, err := h.issuer.Issue(u.ID, u.Email, u.Name, u.Role)
	if err != nil {
		slog.Error("auth/local: issue token after signup", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "account created but token issuance failed; try signing in"})
		return
	}
	c.JSON(http.StatusCreated, tokenResponse{
		Token:     tok,
		ExpiresAt: exp,
		User:      newMeUser(u),
	})
}

// Signin verifies email + password and issues a JWT. Returns a single
// 401 message regardless of whether the email or password was wrong.
func (h *LocalAuthHandler) Signin(c *gin.Context) {
	var req signinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	email, err := local.NormaliseEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": local.ErrInvalidCredentials.Error()})
		return
	}
	u, err := h.users.GetByEmail(c.Request.Context(), email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": local.ErrInvalidCredentials.Error()})
			return
		}
		slog.Error("auth/local: lookup user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if err := local.VerifyPassword(u.PasswordHash, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": local.ErrInvalidCredentials.Error()})
		return
	}
	tok, exp, err := h.issuer.Issue(u.ID, u.Email, u.Name, u.Role)
	if err != nil {
		slog.Error("auth/local: issue token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue session"})
		return
	}
	c.JSON(http.StatusOK, tokenResponse{
		Token:     tok,
		ExpiresAt: exp,
		User:      newMeUser(u),
	})
}

// Me returns the currently authenticated user's profile. Works for
// both local-auth and OIDC sessions because it reads from the gin
// context the middleware populated.
func (h *LocalAuthHandler) Me(c *gin.Context) {
	claims := auth.GetUser(c)
	if claims == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	c.JSON(http.StatusOK, meUser{
		ID:    claims.Subject,
		Email: claims.Email,
		Name:  claims.Name,
		Role:  primaryRole(claims.Roles),
		Roles: claims.Roles,
	})
}

func newMeUser(u *model.User) meUser {
	return meUser{
		ID:    u.ID,
		Email: u.Email,
		Name:  u.Name,
		Role:  u.Role,
		Roles: []string{u.Role},
	}
}

func primaryRole(roles []string) string {
	if len(roles) == 0 {
		return string(auth.RoleViewer)
	}
	return roles[0]
}
