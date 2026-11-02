package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/source/connection"
)

func (h *Handler) ListSourceConnections(c *gin.Context) {
	if h.SourceConnections == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "source connections require the durable job queue"})
		return
	}
	items, err := h.SourceConnections.Store.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cannot read source connections"})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) CreateSourceConnection(c *gin.Context) {
	if h.SourceConnections == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "source connections require the durable job queue"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var req struct {
		connection.Connection
		WebhookSecret string `json:"webhookSecret"`
		Token         string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid source connection"})
		return
	}
	if err := h.SourceConnections.CreateWithCredentials(c.Request.Context(), &req.Connection, connection.Credentials{WebhookSecret: req.WebhookSecret, Token: req.Token}); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, &req.Connection)
}

func (h *Handler) UpdateSourceConnection(c *gin.Context) {
	if h.SourceConnections == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "source connections are unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var req struct {
		connection.Connection
		WebhookSecret *string `json:"webhookSecret"`
		Token         *string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid source connection"})
		return
	}
	req.ID = c.Param("id")
	if err := h.SourceConnections.UpdateWithCredentials(c.Request.Context(), &req.Connection, connection.CredentialsPatch{WebhookSecret: req.WebhookSecret, Token: req.Token}); err != nil {
		if req.Token != nil {
			code, message := sourceTokenFailure(err)
			c.JSON(code, gin.H{"error": message})
			return
		}
		code := http.StatusUnprocessableEntity
		if errors.Is(err, connection.ErrNotFound) {
			code = http.StatusNotFound
		}
		if errors.Is(err, connection.ErrConflict) {
			code = http.StatusConflict
		}
		if errors.Is(err, connection.ErrCredentialCheckUnavailable) {
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, &req.Connection)
}

// ReplaceSourceToken never accepts provider, destination, webhook or allowlist
// changes. A fixed public error keeps replacement values and provider responses
// out of the response even when a custom store/probe returns arbitrary text.
func (h *Handler) ReplaceSourceToken(c *gin.Context) {
	if h.SourceConnections == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "source credential checks are unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var req struct {
		Version  int     `json:"version"`
		Token    string  `json:"token"`
		Username *string `json:"username"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Version <= 0 || req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a replacement token and current connection version are required"})
		return
	}
	value, err := h.SourceConnections.ReplaceToken(c.Request.Context(), c.Param("id"), req.Version, req.Token, req.Username)
	if err != nil {
		status, message := sourceTokenFailure(err)
		c.JSON(status, gin.H{"error": message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"connection": value, "checkedRepositories": len(value.Repositories), "check": "repository_metadata_access"})
}

func sourceTokenFailure(err error) (int, string) {
	switch {
	case errors.Is(err, connection.ErrConflict):
		return http.StatusConflict, "source connection changed; reload its settings before replacing the token"
	case errors.Is(err, connection.ErrNotFound):
		return http.StatusNotFound, "source connection is unavailable"
	case errors.Is(err, connection.ErrCredentialCheckUnavailable):
		return http.StatusServiceUnavailable, "source credential checks are unavailable; saved credentials were not changed"
	case errors.Is(err, connection.ErrCredentialCheck), errors.Is(err, connection.ErrTokenReplacementInput):
		return http.StatusUnprocessableEntity, "replacement was not saved; check the token, username and allowed repository access"
	default:
		// A persistence error can follow an applied write whose acknowledgement
		// was lost. Do not assert that the old credential remains stored.
		return http.StatusServiceUnavailable, "replacement result could not be confirmed; reload this connection before trying again"
	}
}
