package handler

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/entitlements"
	"github.com/santapong/cooker/internal/service"
)

// requireLicense aborts with 503 when the license service was not wired
// (defensive — server.New always sets it). Keeps the handler from
// panicking on a nil service in store-less test Handlers.
func (h *Handler) requireLicense(c *gin.Context) bool {
	if h.License != nil {
		return true
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error": "license service unavailable",
	})
	return false
}

// installLicenseRequest is the POST /license body. The license token is
// the only operator input; the installer identity comes from the
// authenticated claims, never the body.
type installLicenseRequest struct {
	Token string `json:"token"`
}

// licenseStatus is a small derived status string for the UI: "active"
// (license installed and not expired), "expired" (installed but past its
// expiry — entitlements have degraded to Free), or "none" (no license).
const (
	licenseStatusActive  = "active"
	licenseStatusExpired = "expired"
	licenseStatusNone    = "none"
)

// GetLicense returns the current license status and resolved
// entitlements. It is readable by any authenticated user (the frontend
// uses it to show/hide paid features); it never echoes the raw license
// token (model.License.RawToken is json:"-"). With no license installed it
// returns status "none" and the Free entitlements — a 200, not a 404.
func (h *Handler) GetLicense(c *gin.Context) {
	if !h.requireLicense(c) {
		return
	}
	lic, ent, err := h.License.Current(c.Request.Context())
	if err != nil {
		// A store failure still yields Free entitlements; surface 500 so
		// operators notice, but the payload remains usable.
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := gin.H{
		"status":       licenseStatusNone,
		"plan":         ent.Plan,
		"seats":        ent.Seats,
		"features":     featureList(ent),
		"entitlements": entitlementsView(ent),
	}
	if lic != nil {
		status := licenseStatusActive
		if lic.IsExpired(time.Now()) {
			status = licenseStatusExpired
		}
		resp["status"] = status
		resp["customer"] = lic.Customer
		resp["issuedAt"] = lic.IssuedAt
		resp["expiresAt"] = lic.ExpiresAt // nil => perpetual
		resp["installedAt"] = lic.InstalledAt
		resp["installedByEmail"] = lic.InstalledByEmail
	}
	c.JSON(http.StatusOK, resp)
}

// InstallLicense installs a signed license token. Admin-only (enforced by
// auth.RequireRole at the route). The token is verified and rejected if
// invalid or already expired; on success the new status + entitlements are
// returned in the same shape as GetLicense so the UI can update in place.
func (h *Handler) InstallLicense(c *gin.Context) {
	if !h.requireLicense(c) {
		return
	}
	claims := auth.GetUser(c)
	if claims == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	var req installLicenseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	lic, err := h.License.Install(c.Request.Context(), req.Token, claims.Subject, claims.Email)
	if err != nil {
		h.abortLicenseErr(c, err)
		return
	}
	_, ent, _ := h.License.Current(c.Request.Context())
	c.JSON(http.StatusCreated, gin.H{
		"status":           licenseStatusActive,
		"plan":             lic.Plan,
		"seats":            lic.Seats,
		"features":         featureList(ent),
		"entitlements":     entitlementsView(ent),
		"customer":         lic.Customer,
		"issuedAt":         lic.IssuedAt,
		"expiresAt":        lic.ExpiresAt,
		"installedAt":      lic.InstalledAt,
		"installedByEmail": lic.InstalledByEmail,
	})
}

// DeleteLicense removes the installed license, reverting the instance to
// Free. Admin-only (enforced at the route). Idempotent: removing when none
// is installed still returns 200.
func (h *Handler) DeleteLicense(c *gin.Context) {
	if !h.requireLicense(c) {
		return
	}
	if err := h.License.Remove(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": licenseStatusNone, "plan": entitlements.PlanFree})
}

// abortLicenseErr maps license-service sentinels to HTTP status codes.
// Invalid / expired / empty tokens are client errors (400); a disabled
// verifier (no public key configured) is also a 400 with an explanatory
// message so the operator knows to set COOKER_LICENSE_PUBLIC_KEY.
func (h *Handler) abortLicenseErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrLicenseEmpty):
		c.JSON(http.StatusBadRequest, gin.H{"error": "license token is required"})
	case errors.Is(err, service.ErrLicenseExpired):
		c.JSON(http.StatusBadRequest, gin.H{"error": "license token is already expired"})
	case errors.Is(err, service.ErrLicenseInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": "license token is invalid"})
	case errors.Is(err, service.ErrVerificationDisabled):
		c.JSON(http.StatusBadRequest, gin.H{"error": "license verification is disabled; set COOKER_LICENSE_PUBLIC_KEY"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// entitlementsView renders the entitlements feature map as a stable
// object for the frontend. featureList is the sorted slice of unlocked
// feature keys (handy for rendering a badge list).
func entitlementsView(e entitlements.Entitlements) gin.H {
	return gin.H{
		"plan":     e.Plan,
		"seats":    e.Seats,
		"features": e.Features, // map[feature]bool
	}
}

func featureList(e entitlements.Entitlements) []string {
	out := make([]string, 0, len(e.Features))
	for f, on := range e.Features {
		if on {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}
