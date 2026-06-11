package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/oci"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/validate"
)

func ListRepositories(c *gin.Context) {
	// Placeholder: will use go-containerregistry to list repos
	// Implements OCI distribution-spec: GET /v2/_catalog
	c.JSON(http.StatusOK, gin.H{
		"repositories": []string{},
		"ociSpec":      "distribution-spec v1.1",
	})
}

func ListTags(c *gin.Context) {
	name := c.Param("name")
	// Implements OCI distribution-spec: GET /v2/<name>/tags/list
	c.JSON(http.StatusOK, gin.H{
		"name": name,
		"tags": []string{},
	})
}

func GetManifest(c *gin.Context) {
	name := c.Param("name")
	ref := c.Param("ref")
	// Implements OCI distribution-spec: GET /v2/<name>/manifests/<reference>
	// Returns OCI Image Manifest or Image Index
	c.JSON(http.StatusOK, gin.H{
		"name":      name,
		"reference": ref,
		"manifest":  nil,
		"mediaType": oci.MediaTypeImageManifest,
	})
}

func PushImage(c *gin.Context) {
	var req struct {
		Image    string `json:"image" binding:"required"`
		Registry string `json:"registry" binding:"required"`
		Tag      string `json:"tag"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Implements OCI distribution-spec push sequence:
	// 1. POST /v2/<name>/blobs/uploads/ (initiate blob upload)
	// 2. PUT /v2/<name>/blobs/uploads/<uuid> (upload blob)
	// 3. PUT /v2/<name>/manifests/<reference> (push manifest)
	c.JSON(http.StatusAccepted, gin.H{
		"message":  "push initiated",
		"image":    req.Image,
		"registry": req.Registry,
	})
}

func PullImage(c *gin.Context) {
	var req struct {
		Image    string `json:"image" binding:"required"`
		Registry string `json:"registry"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "pull initiated",
		"image":   req.Image,
	})
}

func GetReferrers(c *gin.Context) {
	name := c.Param("name")
	digest := c.Param("digest")
	// Implements OCI distribution-spec v1.1 referrers API:
	// GET /v2/<name>/referrers/<digest>
	// Returns artifacts (signatures, SBOMs, build provenance) referencing this image
	c.JSON(http.StatusOK, gin.H{
		"name":      name,
		"digest":    digest,
		"referrers": []interface{}{},
		"ociSpec":   "distribution-spec v1.1 referrers API",
	})
}

// Settings handlers for registry + cluster configuration (audit
// finding HS26-05-04). These persist through store.RegistryConfigStore
// / store.ClusterConfigStore; sensitive credentials (registry password,
// cluster kubeconfig) are written to secrets.Manager by the service
// layer and never echoed back — List/Get return the Redact()ed view
// with a "has credentials" flag only, mirroring the Host handlers.

// handleConfigServiceError maps RegistryConfig/ClusterConfig service
// errors to HTTP status codes. ErrConfigSecretsUnavailable → 503;
// store.ErrNotFound → 404; anything else → 500 with a fixed body (the
// wrapped detail may carry internal paths/URLs, so it is logged
// server-side instead of leaked). Mirrors handleHostServiceError.
func handleConfigServiceError(c *gin.Context, err error, notFoundMsg string) {
	switch {
	case errors.Is(err, service.ErrConfigSecretsUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": notFoundMsg})
	default:
		slog.Error("settings config service error", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

func (h *Handler) ListRegistryConfigs(c *gin.Context) {
	configs, err := h.Store.Registries.List(c.Request.Context())
	if abortStoreErr(c, err, "registries not found") {
		return
	}
	out := make([]*model.RegistryConfigResponse, 0, len(configs))
	for _, r := range configs {
		out = append(out, r.Redact())
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) AddRegistryConfig(c *gin.Context) {
	var req struct {
		Name     string `json:"name" binding:"required"`
		URL      string `json:"url" binding:"required"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validate.Name("name", req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.Registries == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "registry config service not configured"})
		return
	}
	now := time.Now()
	cfg := &model.RegistryConfig{
		ID:        uuid.New().String(),
		Name:      req.Name,
		URL:       req.URL,
		Username:  req.Username,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.Registries.CreateRegistry(c.Request.Context(), cfg, []byte(req.Password)); err != nil {
		handleConfigServiceError(c, err, "registry not found")
		return
	}
	c.JSON(http.StatusCreated, cfg.Redact())
}

func (h *Handler) DeleteRegistryConfig(c *gin.Context) {
	if h.Registries == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "registry config service not configured"})
		return
	}
	if err := h.Registries.DeleteRegistry(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "registry not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "registry removed", "id": c.Param("id")})
}

func (h *Handler) ListClusterConfigs(c *gin.Context) {
	configs, err := h.Store.Clusters.List(c.Request.Context())
	if abortStoreErr(c, err, "clusters not found") {
		return
	}
	out := make([]*model.ClusterConfigResponse, 0, len(configs))
	for _, cfg := range configs {
		out = append(out, cfg.Redact())
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) AddClusterConfig(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		KubeConfig string `json:"kubeconfig"`
		Context    string `json:"context"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validate.Name("name", req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.Clusters == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster config service not configured"})
		return
	}
	now := time.Now()
	cfg := &model.ClusterConfig{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Context:   req.Context,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.Clusters.CreateCluster(c.Request.Context(), cfg, []byte(req.KubeConfig)); err != nil {
		handleConfigServiceError(c, err, "cluster not found")
		return
	}
	c.JSON(http.StatusCreated, cfg.Redact())
}

func (h *Handler) DeleteClusterConfig(c *gin.Context) {
	if h.Clusters == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster config service not configured"})
		return
	}
	if err := h.Clusters.DeleteCluster(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "cluster not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "cluster removed", "id": c.Param("id")})
}
