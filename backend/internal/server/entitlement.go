package server

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/entitlements"
)

// entitlementResolver is the narrow read surface the gate needs: resolve
// the current install's entitlements. Implemented by
// service.LicenseService.Entitlements (which never errors — it degrades to
// Free). Kept as an interface so the gate is unit-testable with a fake and
// has no service→server import coupling.
type entitlementResolver interface {
	Entitlements(ctx context.Context) entitlements.Entitlements
}

// entitlementGate returns middleware that blocks a request with HTTP 402
// Payment Required when the current install's entitlements do not unlock
// the named feature. The body is {"error":"feature_not_in_plan",
// "feature":"<feature>"} so the frontend can render an upgrade CTA.
//
// IMPORTANT: this middleware is deliberately NOT applied to any existing
// route. Hard-gating functionality that is free today would be a
// regression, and the open-core split (which features become paid) is a
// pending product decision (see docs/launch/01-billing-monetization.md §7
// "Open-core licensing"). This gate is implemented and unit-tested so it
// is ready the moment that decision lands; wiring it onto a route is a
// separate, deliberate change.
//
// A nil resolver fails open (Next) rather than locking every route — a
// missing license service must never break the API.
func entitlementGate(resolver entitlementResolver, feature string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if resolver == nil {
			c.Next()
			return
		}
		ent := resolver.Entitlements(c.Request.Context())
		if !ent.Has(feature) {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":   "feature_not_in_plan",
				"feature": feature,
			})
			return
		}
		c.Next()
	}
}
