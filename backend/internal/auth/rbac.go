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
			"error": "insufficient permissions",
			"required": roles,
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

// MapGroupsToRoles converts OIDC group claims to Cooker roles.
// This mapping is configurable per deployment.
func MapGroupsToRoles(groups []string) []string {
	roleMap := map[string]string{
		"cooker-admins":    string(RoleAdmin),
		"cooker-operators": string(RoleOperator),
		"cooker-approvers": string(RoleApprover),
		"cooker-viewers":   string(RoleViewer),
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
