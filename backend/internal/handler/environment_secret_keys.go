package handler

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
)

const secretKeyMetadataTimeout = 5 * time.Second
const secretKeyMetadataLimit = 10000

// ListEnvironmentSecretKeys lists names, never values. Environment generation
// fences configuration and local database-secret changes; external backends do
// not claim this conditional-edit capability. Provider errors are fixed/safe.
func (h *Handler) ListEnvironmentSecretKeys(c *gin.Context) {
	c.Header("Cache-Control", "no-store, private")
	if h.Secrets == nil {
		secretKeysUnavailable(c)
		return
	}
	env, ok := h.publicEnvironment(c, c.Param("id"))
	if !ok {
		return
	}
	generation := env.Version
	ctx, cancel := context.WithTimeout(c.Request.Context(), secretKeyMetadataTimeout)
	defer cancel()
	keys, err := h.Secrets.List(ctx, env.ID)
	if err != nil || ctx.Err() != nil || len(keys) > secretKeyMetadataLimit {
		secretKeysUnavailable(c)
		return
	}
	// Do not sort provider-owned slices in place. Never use Get/reveal here.
	out := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if len(key) == 0 || len(key) > 256 || !utf8.ValidString(key) || strings.ContainsAny(key, "\x00\r\n") {
			secretKeysUnavailable(c)
			return
		}
		if !seen[key] {
			out = append(out, key)
			seen[key] = true
		}
	}
	// Keys must correspond to the returned generation. A changed environment
	// cannot be accepted as an empty or current key inventory.
	current, err := h.Store.Environments.Get(ctx, env.ID)
	if err != nil || current == nil || ctx.Err() != nil {
		secretKeysUnavailable(c)
		return
	}
	if current.Version != generation {
		c.JSON(http.StatusConflict, gin.H{"error": "environment generation changed; refresh before editing", "sourceAvailability": "unavailable"})
		return
	}
	if model.IsRegistryCredentialEnvironment(current) {
		c.JSON(http.StatusNotFound, gin.H{"error": "environment not found"})
		return
	}
	sort.Strings(out)
	_, conditional := h.Secrets.(secrets.ConditionalEditor)
	c.JSON(http.StatusOK, gin.H{
		"keys":               out,
		"sourceAvailability": "available",
		"generation":         generation,
		"conditionalEditing": conditional,
	})
}

func secretKeysUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error":              "secret key metadata unavailable",
		"sourceAvailability": "unavailable",
		"conditionalEditing": false,
	})
}
