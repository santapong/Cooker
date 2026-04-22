// Package handler contains HTTP handlers for Cooker's REST API. The
// Handler struct owns persistence through a *store.Store; router code
// constructs one instance and binds its methods as gin.HandlerFunc.
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/crypto"
	"github.com/cooker-ci/cooker/internal/service"
	"github.com/cooker-ci/cooker/internal/store"
)

// Handler owns the dependencies shared by request handlers.
type Handler struct {
	Store       *store.Store
	Codec       *crypto.Codec
	AppDeployer *service.AppDeployer
	// WSBroadcast, when non-nil, publishes progress events to a
	// WebSocket channel. Wired by the server to
	// (*server.WebSocketHub).Broadcast.
	WSBroadcast func(channel string, data []byte)
}

// New constructs a Handler bound to the given store. Codec is
// optional; when nil or inactive the secret endpoints return 503 so
// the operator knows to set COOKER_SECRET_KEY.
func New(s *store.Store, codec *crypto.Codec) *Handler {
	return &Handler{Store: s, Codec: codec}
}

// abortStoreErr maps common store errors to HTTP responses. Returns
// true when it wrote a response so the caller can return early.
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
