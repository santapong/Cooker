package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/secrets"
)

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
		// BC-H4: don't leak backend URLs/ARNs/paths via the error message;
		// log server-side and return a generic message (mirrors RevealSecret).
		slog.Warn("handler: PutSecret: backend error", "env", c.Param("id"), "key", c.Param("key"), "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "secret backend error"})
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
	// Strict no-store: a leaked browser cache or a forward proxy
	// shouldn't be holding a copy of the plaintext value.
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
	c.Header("Pragma", "no-cache")
	if _, err := h.Store.Environments.Get(c.Request.Context(), c.Param("id")); abortStoreErr(c, err, "environment not found") {
		return
	}
	value, err := h.Secrets.Get(c.Request.Context(), c.Param("id"), c.Param("key"))
	if errors.Is(err, secrets.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "secret not found"})
		return
	}
	if err != nil {
		// Generic message: the upstream secrets-manager error can leak
		// Vault path / AWS ARN / KeepSave URL detail.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "secret backend error"})
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
		// BC-H4: don't leak backend URLs/ARNs/paths via the error message;
		// log server-side and return a generic message (mirrors RevealSecret).
		slog.Warn("handler: DeleteSecret: backend error", "env", c.Param("id"), "key", c.Param("key"), "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "secret backend error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": c.Param("key"), "status": "deleted"})
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
