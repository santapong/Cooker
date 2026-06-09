package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/service"
)

// testSecretsRequest is the optional JSON body of POST
// /settings/secrets/test. Omitting environmentId probes against the
// first environment in the store.
type testSecretsRequest struct {
	EnvironmentID string `json:"environmentId,omitempty"`
}

// TestSecretsBackend serves POST /settings/secrets/test (roadmap M5 /
// F12): an admin-triggered connectivity probe of the configured
// secrets backend. The response carries the backend kind, latency,
// and a classified error — never key names or values. Always 200
// when the probe ran; probe failure is data, not an HTTP error.
func (h *Handler) TestSecretsBackend(c *gin.Context) {
	if h.Secrets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "secrets backend not configured"})
		return
	}
	var req testSecretsRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	envID := req.EnvironmentID
	if envID == "" {
		envs, err := h.Store.Environments.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(envs) == 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "no environments to probe against — create one or pass environmentId"})
			return
		}
		envID = envs[0].ID
	}
	c.JSON(http.StatusOK, service.CheckSecrets(c.Request.Context(), h.Secrets, h.SecretsBackend, envID))
}
