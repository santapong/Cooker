package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/provider/vercel"
)

func (h *Handler) landingUpdateReviewAccess(c *gin.Context, write bool) bool {
	if !h.landingAdoptionAccess(c, write) {
		return false
	}
	if h.VercelUpdateReviews == nil {
		vercelError(c, vercel.ErrUnavailable)
		return false
	}
	return true
}

func (h *Handler) ListVercelLandingUpdateReviews(c *gin.Context) {
	if !h.landingUpdateReviewAccess(c, false) {
		return
	}
	rows, err := h.VercelUpdateReviews.List(c.Request.Context())
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"reviews": rows, "historyLimit": vercel.LandingUpdateReviewHistoryLimit})
}

func (h *Handler) GetVercelLandingUpdateReview(c *gin.Context) {
	if !h.landingUpdateReviewAccess(c, false) {
		return
	}
	review, err := h.VercelUpdateReviews.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"review": review})
}

func (h *Handler) CreateVercelLandingUpdateReview(c *gin.Context) {
	if !h.landingUpdateReviewAccess(c, true) {
		return
	}
	var request vercel.LandingUpdateReviewRequest
	if !decodeLandingAdoption(c, &request) {
		return
	}
	review, err := h.VercelUpdateReviews.Create(c.Request.Context(), request, auth.GetUser(c).Subject)
	if err != nil {
		vercelError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"review": review})
}
