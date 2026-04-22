package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cooker-ci/cooker/internal/auth"
	"github.com/cooker-ci/cooker/internal/model"
)

// ListEnvironments returns all environments with secrets redacted so
// any authenticated user can safely inspect structure.
func (h *Handler) ListEnvironments(c *gin.Context) {
	envs, err := h.Store.Environments.List(c.Request.Context())
	if abortStoreErr(c, err, "environments not found") {
		return
	}
	out := make([]*model.Environment, 0, len(envs))
	for _, e := range envs {
		out = append(out, e.Redact())
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) CreateEnvironment(c *gin.Context) {
	var env model.Environment
	if err := c.ShouldBindJSON(&env); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// New environments never carry secrets on create; use the dedicated
	// /secrets endpoint so ciphertext never flows through general
	// CRUD handlers.
	env.Secrets = nil
	env.ID = uuid.New().String()
	env.CreatedAt = time.Now()
	if env.PlainVars == nil {
		// Back-compat: accept the legacy `variables` field from clients
		// that haven't migrated yet.
		env.PlainVars = env.Variables
	}
	if env.PlainVars == nil {
		env.PlainVars = make(map[string]string)
	}
	env.Variables = nil

	if err := h.Store.Environments.Create(c.Request.Context(), &env); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, env.Redact())
}

func (h *Handler) UpdateEnvironment(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.Store.Environments.Get(c.Request.Context(), id)
	if abortStoreErr(c, err, "environment not found") {
		return
	}

	var env model.Environment
	if err := c.ShouldBindJSON(&env); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	env.ID = id
	env.CreatedAt = existing.CreatedAt
	// Preserve secrets across PUT — clients update secrets via the
	// dedicated endpoint, never via the general PUT body.
	env.Secrets = existing.Secrets
	if env.PlainVars == nil {
		env.PlainVars = env.Variables
	}
	env.Variables = nil

	if err := h.Store.Environments.Update(c.Request.Context(), &env); err != nil {
		if abortStoreErr(c, err, "environment not found") {
			return
		}
	}
	c.JSON(http.StatusOK, env.Redact())
}

func (h *Handler) DeleteEnvironment(c *gin.Context) {
	if err := h.Store.Environments.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "environment not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "environment deleted"})
}

// PutSecret upserts a single secret for an environment. Admin only.
// The request body is {"value":"..."}; the value is AES-GCM sealed
// with the handler's codec before it hits the store.
func (h *Handler) PutSecret(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	claims := auth.GetUser(c)
	if !auth.CanRevealSecret(claims) {
		// Writing a secret is equally sensitive as revealing one.
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return
	}
	var req struct {
		Value string `json:"value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	env, err := h.Store.Environments.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "environment not found") {
		return
	}
	sealed, err := h.Codec.Seal([]byte(req.Value))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "seal: " + err.Error()})
		return
	}
	if env.Secrets == nil {
		env.Secrets = make(map[string][]byte)
	}
	env.Secrets[c.Param("key")] = sealed
	if err := h.Store.Environments.Update(c.Request.Context(), env); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": c.Param("key"), "status": "stored"})
}

// RevealSecret decrypts and returns a single secret's plaintext.
// Admin only. Redacted values come from the regular GET endpoints.
func (h *Handler) RevealSecret(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	claims := auth.GetUser(c)
	if !auth.CanRevealSecret(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return
	}
	env, err := h.Store.Environments.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "environment not found") {
		return
	}
	sealed, ok := env.Secrets[c.Param("key")]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "secret not found"})
		return
	}
	plain, err := h.Codec.Open(sealed)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "open: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": c.Param("key"), "value": string(plain)})
}

// DeleteSecret removes a secret key from the environment. Admin only.
func (h *Handler) DeleteSecret(c *gin.Context) {
	claims := auth.GetUser(c)
	if !auth.CanRevealSecret(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return
	}
	env, err := h.Store.Environments.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "environment not found") {
		return
	}
	delete(env.Secrets, c.Param("key"))
	if err := h.Store.Environments.Update(c.Request.Context(), env); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": c.Param("key"), "status": "deleted"})
}

func (h *Handler) PromoteRun(c *gin.Context) {
	runID := c.Param("runId")
	c.JSON(http.StatusOK, gin.H{
		"message": "promotion initiated",
		"runId":   runID,
	})
}

// ApprovePromotion records an approval for a promotion. Requires the
// caller to hold RoleAdmin or RoleApprover; the approver identity is
// taken from the OIDC claims, not the request body, so an attacker
// with a viewer token cannot forge an admin-looking approval.
func (h *Handler) ApprovePromotion(c *gin.Context) {
	claims := auth.GetUser(c)
	if !auth.CanApprovePromotion(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "requires admin or approver role",
		})
		return
	}

	runID := c.Param("runId")
	// Accept an optional note; ignore any "approvedBy" field in the
	// body — the trusted identity is claims.Email.
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)

	c.JSON(http.StatusOK, gin.H{
		"message":    "promotion approved",
		"runId":      runID,
		"approvedBy": claims.Email,
		"note":       req.Note,
	})
}

func (h *Handler) GetEnvStatus(c *gin.Context) {
	runID := c.Param("runId")
	c.JSON(http.StatusOK, gin.H{
		"runId":    runID,
		"statuses": []model.EnvironmentStatus{},
	})
}

// requireCodec aborts with 503 if no secret key is configured. The
// operator needs to set COOKER_SECRET_KEY before secrets work.
func (h *Handler) requireCodec(c *gin.Context) bool {
	if h.Codec != nil && h.Codec.Active() {
		return true
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error": "secrets disabled: set COOKER_SECRET_KEY",
	})
	return false
}
