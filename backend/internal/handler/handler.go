// Package handler contains HTTP handlers for Cooker's REST API. The
// Handler struct owns persistence through a *store.Store; router code
// constructs one instance and binds its methods as gin.HandlerFunc.
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/crypto"
	"github.com/cooker-ci/cooker/internal/secrets"
	"github.com/cooker-ci/cooker/internal/service"
	"github.com/cooker-ci/cooker/internal/store"
)

// Handler owns the dependencies shared by request handlers.
type Handler struct {
	Store *store.Store
	// Codec is retained for backward compatibility with handlers that
	// still reach for AES-GCM directly; new code should go through
	// Secrets instead. Secret endpoints (Put/Reveal/Delete) delegate
	// to Secrets and ignore Codec.
	Codec       *crypto.Codec
	Secrets     secrets.Manager
	AppDeployer *service.AppDeployer
	WSBroadcast func(channel string, data []byte)
}

// New constructs a Handler bound to the given store. secs may be nil
// when no secrets backend is configured (dev mode with backend=database
// and no COOKER_SECRET_KEY set); the secret endpoints will return 503.
func New(s *store.Store, codec *crypto.Codec, secs secrets.Manager) *Handler {
	return &Handler{Store: s, Codec: codec, Secrets: secs}
}

// abortStoreErr maps common store errors to HTTP responses.
func abortStoreErr(c *gin.Context, err error, notFoundMsg string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": notFoundMsg})
		return true
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	return true
}
