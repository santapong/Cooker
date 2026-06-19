package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/service"
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

// PromoteRun records a promotion of a pipeline run to a target
// environment. The route is operator+ (writeRole); the target env's
// PromotionPolicy decides whether the promotion auto-completes or waits
// for approval. Persists through the Promoter service — this handler
// only parses, authorizes-by-route, and shapes the response.
func (h *Handler) PromoteRun(c *gin.Context) {
	if !h.requirePromotions(c) {
		return
	}
	run, ok := h.loadRunForPipeline(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	var req struct {
		EnvironmentID string `json:"environmentId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "environmentId is required"})
		return
	}

	var requestedBy string
	if claims := auth.GetUser(c); claims != nil {
		requestedBy = claims.Email
	}

	promo, err := h.Promotions.Promote(c.Request.Context(), run.ID, run.PipelineID, req.EnvironmentID, requestedBy)
	if err != nil {
		if abortStoreErr(c, err, "environment not found") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"runId":         run.ID,
		"environmentId": promo.EnvironmentID,
		"status":        promo.Status,
		"promotion":     promo,
	})
}

// ApprovePromotion records an approval for a run's promotion to a target
// environment. Requires the caller to hold RoleAdmin or RoleApprover;
// the approver identity is taken from the OIDC claims, not the request
// body (IDOR-safe). The promotion advances once the distinct-approver
// count reaches the env policy's RequiredApprovers.
func (h *Handler) ApprovePromotion(c *gin.Context) {
	claims := auth.GetUser(c)
	if !auth.CanApprovePromotion(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "requires admin or approver role",
		})
		return
	}
	if !h.requirePromotions(c) {
		return
	}

	run, ok := h.loadRunForPipeline(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	var req struct {
		EnvironmentID string `json:"environmentId" binding:"required"`
		Note          string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "environmentId is required"})
		return
	}

	promo, err := h.Promotions.Approve(c.Request.Context(), run.ID, req.EnvironmentID, claims.Subject, claims.Email, req.Note)
	if err != nil && !errors.Is(err, service.ErrAlreadyApproved) {
		if abortStoreErr(c, err, "promotion not found") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"runId":         run.ID,
		"environmentId": promo.EnvironmentID,
		"status":        promo.Status,
		"approvedBy":    claims.Email,
		"note":          req.Note,
		"approvalsHave": promo.ApprovalCount(),
		"approvalsNeed": promo.RequiredApprovers,
		"promotion":     promo,
	})
}

// GetEnvStatus returns the real per-environment promotion status for a
// run, derived from the persisted promotion rows (HS26-05-08).
func (h *Handler) GetEnvStatus(c *gin.Context) {
	if !h.requirePromotions(c) {
		return
	}
	run, ok := h.loadRunForPipeline(c, c.Param("runId"), c.Param("id"))
	if !ok {
		return
	}
	statuses, err := h.Promotions.Statuses(c.Request.Context(), run.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"runId":    run.ID,
		"statuses": statuses,
	})
}

// requirePromotions aborts with 503 when the promotion service was not
// wired (defensive — server.New always sets it; a nil here means a
// store-less test Handler). Keeps the handler from panicking on nil.
func (h *Handler) requirePromotions(c *gin.Context) bool {
	if h.Promotions != nil {
		return true
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error": "promotion service unavailable",
	})
	return false
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
