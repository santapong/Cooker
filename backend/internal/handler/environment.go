package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cooker-ci/cooker/internal/auth"
	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/secrets"
)

// ListEnvironments returns all environments with secrets redacted so
// any authenticated user can safely inspect structure.
//
// @Summary      List environments
// @Tags         environments
// @Produce      json
// @Success      200  {array}   model.Environment
// @Failure      401  {object}  map[string]string
// @Security     BearerAuth
// @Router       /environments [get]
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
	env.Secrets = nil
	env.ID = uuid.New().String()
	env.CreatedAt = time.Now()
	if env.PlainVars == nil {
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
// Storage is delegated to the configured secrets.Manager (database
// or KeepSave); this handler only authorizes and routes.
//
// @Summary      Upsert a secret value
// @Tags         secrets
// @Accept       json
// @Produce      json
// @Param        id    path      string                 true  "Environment ID"
// @Param        key   path      string                 true  "Secret key"
// @Param        body  body      object{value=string}   true  "New secret value"
// @Success      200   {object}  map[string]string
// @Failure      400   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Security     BearerAuth
// @Router       /environments/{id}/secrets/{key} [put]
func (h *Handler) PutSecret(c *gin.Context) {
	if !h.requireSecrets(c) {
		return
	}
	claims := auth.GetUser(c)
	if !auth.CanRevealSecret(claims) {
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
	if _, err := h.Store.Environments.Get(c.Request.Context(), c.Param("id")); abortStoreErr(c, err, "environment not found") {
		return
	}
	if err := h.Secrets.Put(c.Request.Context(), c.Param("id"), c.Param("key"), []byte(req.Value)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": c.Param("key"), "status": "stored"})
}

// RevealSecret returns a single secret's plaintext. Admin only.
func (h *Handler) RevealSecret(c *gin.Context) {
	if !h.requireSecrets(c) {
		return
	}
	claims := auth.GetUser(c)
	if !auth.CanRevealSecret(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return
	}
	if _, err := h.Store.Environments.Get(c.Request.Context(), c.Param("id")); abortStoreErr(c, err, "environment not found") {
		return
	}
	value, err := h.Secrets.Get(c.Request.Context(), c.Param("id"), c.Param("key"))
	if errors.Is(err, secrets.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "secret not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": c.Param("key"), "value": string(value)})
}

// PromoteSecrets copies secrets from the URL-path environment to the
// environment named in the request body. Admin only. Backend support
// is optional; backends that do not implement secrets.Promoter
// produce 501.
//
// @Summary      Promote secrets between environments
// @Tags         secrets
// @Accept       json
// @Produce      json
// @Param        id    path      string  true  "Source environment ID"
// @Param        body  body      object{toEnvironmentId=string,keys=[]string}  true  "Promotion target"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      501   {object}  map[string]string  "Backend does not support promotion"
// @Security     BearerAuth
// @Router       /environments/{id}/secrets/promote [post]
func (h *Handler) PromoteSecrets(c *gin.Context) {
	if !h.requireSecrets(c) {
		return
	}
	claims := auth.GetUser(c)
	if !auth.CanRevealSecret(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return
	}
	var req struct {
		ToEnvironmentID string   `json:"toEnvironmentId" binding:"required"`
		Keys            []string `json:"keys"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	from := c.Param("id")
	if from == req.ToEnvironmentID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source and target environments must differ"})
		return
	}
	if _, err := h.Store.Environments.Get(c.Request.Context(), from); abortStoreErr(c, err, "source environment not found") {
		return
	}
	if _, err := h.Store.Environments.Get(c.Request.Context(), req.ToEnvironmentID); abortStoreErr(c, err, "target environment not found") {
		return
	}
	promoter, ok := h.Secrets.(secrets.Promoter)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": secrets.ErrPromotionUnsupported.Error()})
		return
	}
	if err := promoter.Promote(c.Request.Context(), from, req.ToEnvironmentID, req.Keys); err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"from":   from,
		"to":     req.ToEnvironmentID,
		"keys":   req.Keys,
		"status": "promoted",
	})
}

// DeleteSecret removes a secret key from the environment. Admin only.
func (h *Handler) DeleteSecret(c *gin.Context) {
	if !h.requireSecrets(c) {
		return
	}
	claims := auth.GetUser(c)
	if !auth.CanRevealSecret(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return
	}
	if _, err := h.Store.Environments.Get(c.Request.Context(), c.Param("id")); abortStoreErr(c, err, "environment not found") {
		return
	}
	if err := h.Secrets.Delete(c.Request.Context(), c.Param("id"), c.Param("key")); err != nil {
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
// taken from the OIDC claims, not the request body.
func (h *Handler) ApprovePromotion(c *gin.Context) {
	claims := auth.GetUser(c)
	if !auth.CanApprovePromotion(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "requires admin or approver role",
		})
		return
	}

	runID := c.Param("runId")
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

// requireSecrets aborts with 503 when no secrets backend is
// configured. Used by environment secret CRUD which delegates to
// the secrets.Manager. For backend=database this triggers when
// COOKER_SECRET_KEY is empty; for backend=keepsave it should never
// trigger because boot validation rejects partial config.
func (h *Handler) requireSecrets(c *gin.Context) bool {
	if h.Secrets != nil {
		return true
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error": "secrets disabled: set COOKER_SECRET_KEY (database backend) or configure COOKER_SECRETS_BACKEND=keepsave",
	})
	return false
}

// requireCodec aborts with 503 when no AES codec is configured.
// Distinct from requireSecrets: the codec also encrypts data outside
// the secrets.Manager (App webhook secrets), so app webhook handlers
// gate on this even when the secrets backend is keepsave.
func (h *Handler) requireCodec(c *gin.Context) bool {
	if h.Codec != nil && h.Codec.Active() {
		return true
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error": "codec disabled: set COOKER_SECRET_KEY",
	})
	return false
}
