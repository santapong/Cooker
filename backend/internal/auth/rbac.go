package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Role represents a user's access level within Cooker.
type Role string

const (
	RoleAdmin    Role = "admin"    // Full access: manage pipelines, deploy to prod, configure settings
	RoleOperator Role = "operator" // Can run pipelines, manage environments
	RoleApprover Role = "approver" // Narrow role dedicated to promotion approval
	RoleViewer   Role = "viewer"   // Read-only access to all resources
)

// RequireRole returns middleware that checks if the user has one of the required roles.
func RequireRole(roles ...Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := GetUser(c)
		if claims == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			return
		}

		for _, required := range roles {
			for _, userRole := range claims.Roles {
				if userRole == string(required) {
					c.Next()
					return
				}
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error":    "insufficient permissions",
			"required": roles,
		})
	}
}

// RequireMFA returns middleware that enforces step-up authentication.
// The supplied acrValues are matched against the token's `acr` claim
// and `amr` array; either match passes. Empty acrValues disables the
// gate (passthrough) so operators who haven't configured MFA at the
// IdP keep working unchanged.
//
// On failure responds 403 with {"error":"mfa_required","acr_values":...}
// so the frontend can re-issue an OIDC redirect with `acr_values=<v>`.
func RequireMFA(acrValues ...string) gin.HandlerFunc {
	if len(acrValues) == 0 {
		return func(c *gin.Context) { c.Next() }
	}
	allowed := make(map[string]bool, len(acrValues))
	for _, v := range acrValues {
		if v != "" {
			allowed[v] = true
		}
	}
	return func(c *gin.Context) {
		claims := GetUser(c)
		if claims == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			return
		}
		if allowed[claims.ACR] {
			c.Next()
			return
		}
		for _, m := range claims.AMR {
			if allowed[m] {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error":      "mfa_required",
			"acr_values": acrValues,
		})
	}
}

// CanApprovePromotion checks if the user has permission to approve
// environment promotions. Admins and dedicated approvers qualify;
// operators and viewers do not (operators can still trigger runs).
func CanApprovePromotion(claims *Claims) bool {
	if claims == nil {
		return false
	}
	for _, role := range claims.Roles {
		if role == string(RoleAdmin) || role == string(RoleApprover) {
			return true
		}
	}
	return false
}

// CanRevealSecret returns true only for admins. Operators and
// approvers see redacted "***" values.
func CanRevealSecret(claims *Claims) bool {
	if claims == nil {
		return false
	}
	for _, role := range claims.Roles {
		if role == string(RoleAdmin) {
			return true
		}
	}
	return false
}

// DefaultGroupRoleMap is the built-in mapping used when no operator
// override is supplied via COOKER_OIDC_GROUP_MAP.
var DefaultGroupRoleMap = map[string]string{
	"cooker-admins":    string(RoleAdmin),
	"cooker-operators": string(RoleOperator),
	"cooker-approvers": string(RoleApprover),
	"cooker-viewers":   string(RoleViewer),
}

// MapGroupsToRoles converts OIDC group claims to Cooker roles using
// the default mapping. Equivalent to MapGroupsToRolesWith(groups, nil).
func MapGroupsToRoles(groups []string) []string {
	return MapGroupsToRolesWith(groups, nil)
}

// MapGroupsToRolesWith converts OIDC group claims to Cooker roles
// using the supplied mapping. A nil or empty mapping falls back to
// DefaultGroupRoleMap. Unknown groups are ignored; users with no
// matching group default to RoleViewer.
func MapGroupsToRolesWith(groups []string, roleMap map[string]string) []string {
	if len(roleMap) == 0 {
		roleMap = DefaultGroupRoleMap
	}

	var roles []string
	seen := make(map[string]bool)
	for _, group := range groups {
		if role, ok := roleMap[group]; ok && !seen[role] {
			roles = append(roles, role)
			seen[role] = true
		}
	}

	if len(roles) == 0 {
		roles = append(roles, string(RoleViewer))
	}
	return roles
}
