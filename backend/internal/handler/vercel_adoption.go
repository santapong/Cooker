package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/provider/vercel"
)

// The router applies its existing authenticated/admin groups. This check also
// protects a future route registration from accidentally exposing authority edits.
func (h *Handler) landingAdoptionAccess(c *gin.Context, write bool) bool {
	user := auth.GetUser(c)
	if user == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return false
	}
	if write {
		admin := false
		for _, role := range user.Roles {
			if role == string(auth.RoleAdmin) {
				admin = true
				break
			}
		}
		if !admin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "administrator access required"})
			return false
		}
	}
	if h.VercelAdoptions == nil {
		vercelError(c, vercel.ErrUnavailable)
		return false
	}
	return true
}

func decodeLandingAdoption(c *gin.Context, out any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	body, err := io.ReadAll(c.Request.Body)
	trimmed := bytes.TrimSpace(body)
	if err != nil || len(trimmed) == 0 || trimmed[0] != '{' {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid landing adoption request"})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid landing adoption request"})
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid landing adoption request"})
		return false
	}
	return true
}

func (h *Handler) ListVercelLandingAdoptions(c *gin.Context) {
	if !h.landingAdoptionAccess(c, false) {
		return
	}
	items, err := h.VercelAdoptions.List(c.Request.Context())
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"adoptions": items})
}

func (h *Handler) GetVercelLandingAdoption(c *gin.Context) {
	if !h.landingAdoptionAccess(c, false) {
		return
	}
	item, err := h.VercelAdoptions.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"adoption": item})
}

func (h *Handler) CreateVercelLandingAdoption(c *gin.Context) {
	if !h.landingAdoptionAccess(c, true) {
		return
	}
	var request vercel.LandingAdoptionRequest
	if !decodeLandingAdoption(c, &request) {
		return
	}
	item, err := h.VercelAdoptions.Create(c.Request.Context(), request, auth.GetUser(c).Subject)
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"adoption": item})
}

func (h *Handler) UpdateVercelLandingAdoption(c *gin.Context) {
	if !h.landingAdoptionAccess(c, true) {
		return
	}
	var request vercel.LandingAdoptionUpdate
	if !decodeLandingAdoption(c, &request) {
		return
	}
	item, err := h.VercelAdoptions.Update(c.Request.Context(), c.Param("id"), request, auth.GetUser(c).Subject)
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"adoption": item})
}
