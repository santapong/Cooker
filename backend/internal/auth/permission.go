package auth

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Resource is a typed handle for a resource kind in the permission
// matrix. The string value is also what /audit and /metrics emit
// (so renaming a Resource is a breaking change for dashboards — add
// new ones rather than rename).
type Resource string

const (
	ResourcePipeline    Resource = "pipeline"
	ResourceRun         Resource = "run"
	ResourceApp         Resource = "app"
	ResourceEnvironment Resource = "environment"
	ResourceSecret      Resource = "secret"
	ResourceHost        Resource = "host"
	ResourceRegistry    Resource = "registry"
	ResourceCluster     Resource = "cluster"
	ResourceDocker      Resource = "docker"
	ResourceKubernetes  Resource = "kubernetes"
	ResourcePromotion   Resource = "promotion"
	ResourceWebhook     Resource = "webhook"
)

// Action names what the caller is trying to do to a Resource. Read
// covers GETs / Lists; the rest are per-write semantics. Reveal is
// the exception: it's a read of secret material that we want to
// distinguish from List because viewers can List but only admins
// can Reveal.
type Action string

const (
	ActionRead    Action = "read"
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionDelete  Action = "delete"
	ActionReveal  Action = "reveal"
	ActionApprove Action = "approve"
	ActionInvoke  Action = "invoke" // run a pipeline, trigger an app deploy, fire a webhook
)

// policyEntry is one row in the matrix. allowedRoles is the set of
// Roles permitted to perform action on resource. Empty = nobody
// (which is the safe default before a policy is declared).
type policyEntry struct {
	resource     Resource
	action       Action
	allowedRoles []Role
}

// defaultPolicy is the built-in resource-action matrix. Tests load
// it via Policy() so an operator policy override (future work)
// doesn't require duplicating the table.
var defaultPolicy = []policyEntry{
	// Read access — everyone authenticated can list / get.
	{ResourcePipeline, ActionRead, []Role{RoleAdmin, RoleOperator, RoleApprover, RoleViewer}},
	{ResourceRun, ActionRead, []Role{RoleAdmin, RoleOperator, RoleApprover, RoleViewer}},
	{ResourceApp, ActionRead, []Role{RoleAdmin, RoleOperator, RoleApprover, RoleViewer}},
	{ResourceEnvironment, ActionRead, []Role{RoleAdmin, RoleOperator, RoleApprover, RoleViewer}},
	{ResourceHost, ActionRead, []Role{RoleAdmin, RoleOperator, RoleApprover, RoleViewer}},
	{ResourceRegistry, ActionRead, []Role{RoleAdmin, RoleOperator, RoleApprover, RoleViewer}},
	{ResourceCluster, ActionRead, []Role{RoleAdmin, RoleOperator, RoleApprover, RoleViewer}},
	{ResourceDocker, ActionRead, []Role{RoleAdmin, RoleOperator, RoleApprover, RoleViewer}},
	{ResourceKubernetes, ActionRead, []Role{RoleAdmin, RoleOperator, RoleApprover, RoleViewer}},
	// Write access — admin + operator (matches the writeRole shortcut
	// already used in router.go).
	{ResourcePipeline, ActionCreate, []Role{RoleAdmin, RoleOperator}},
	{ResourcePipeline, ActionUpdate, []Role{RoleAdmin, RoleOperator}},
	{ResourcePipeline, ActionInvoke, []Role{RoleAdmin, RoleOperator}},
	{ResourceRun, ActionUpdate, []Role{RoleAdmin, RoleOperator}}, // cancel / cleanup
	{ResourceApp, ActionCreate, []Role{RoleAdmin, RoleOperator}},
	{ResourceApp, ActionUpdate, []Role{RoleAdmin, RoleOperator}},
	{ResourceApp, ActionInvoke, []Role{RoleAdmin, RoleOperator}},
	{ResourceEnvironment, ActionCreate, []Role{RoleAdmin, RoleOperator}},
	{ResourceEnvironment, ActionUpdate, []Role{RoleAdmin, RoleOperator}},
	{ResourceHost, ActionCreate, []Role{RoleAdmin, RoleOperator}},
	{ResourceHost, ActionUpdate, []Role{RoleAdmin, RoleOperator}},
	// Destructive deletes — admin only (matches adminRole shortcut).
	{ResourcePipeline, ActionDelete, []Role{RoleAdmin}},
	{ResourceApp, ActionDelete, []Role{RoleAdmin}},
	{ResourceEnvironment, ActionDelete, []Role{RoleAdmin}},
	{ResourceHost, ActionDelete, []Role{RoleAdmin}},
	{ResourceRegistry, ActionCreate, []Role{RoleAdmin}},
	{ResourceRegistry, ActionDelete, []Role{RoleAdmin}},
	{ResourceCluster, ActionCreate, []Role{RoleAdmin}},
	{ResourceCluster, ActionDelete, []Role{RoleAdmin}},
	// Secrets — reveal/update/delete are admin only; promotion is
	// admin + approver.
	{ResourceSecret, ActionReveal, []Role{RoleAdmin}},
	{ResourceSecret, ActionUpdate, []Role{RoleAdmin}},
	{ResourceSecret, ActionDelete, []Role{RoleAdmin}},
	{ResourcePromotion, ActionApprove, []Role{RoleAdmin, RoleApprover}},
	{ResourcePromotion, ActionInvoke, []Role{RoleAdmin, RoleOperator}},
	// Webhook rotation is admin only; webhook invocations are
	// HMAC-authed and bypass this matrix entirely.
	{ResourceWebhook, ActionUpdate, []Role{RoleAdmin}},
}

// policyMatrix turns the slice into a (resource, action) → role-set
// lookup. Built once at package init; lookups are read-only.
var policyMatrix = func() map[policyKey]map[Role]bool {
	m := make(map[policyKey]map[Role]bool, len(defaultPolicy))
	for _, p := range defaultPolicy {
		key := policyKey{resource: p.resource, action: p.action}
		roles := make(map[Role]bool, len(p.allowedRoles))
		for _, r := range p.allowedRoles {
			roles[r] = true
		}
		m[key] = roles
	}
	return m
}()

type policyKey struct {
	resource Resource
	action   Action
}

// Permission reports whether the supplied claims may perform action
// on resource. nil claims (unauthenticated) is always false. An
// undeclared (resource, action) pair is also false — callers must
// add the row explicitly, not rely on an implicit allow.
func Permission(claims *Claims, resource Resource, action Action) bool {
	if claims == nil {
		return false
	}
	allowed, ok := policyMatrix[policyKey{resource: resource, action: action}]
	if !ok {
		return false
	}
	for _, role := range claims.Roles {
		if allowed[Role(role)] {
			return true
		}
	}
	return false
}

// RequirePermission returns Gin middleware that enforces a
// resource-action check. On deny it logs and aborts with 403; on
// missing claims it aborts with 401. Apply at route registration,
// alongside or instead of RequireRole, when the route's policy
// expresses better as a resource-action match.
//
//	api.POST("/secrets/:key",
//	  auth.RequirePermission(auth.ResourceSecret, auth.ActionUpdate),
//	  h.PutSecret)
func RequirePermission(resource Resource, action Action) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := GetUser(c)
		if claims == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			return
		}
		if !Permission(claims, resource, action) {
			slog.Info("permission denied",
				"sub", claims.Subject,
				"roles", claims.Roles,
				"resource", resource,
				"action", action,
				"path", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":    "insufficient permissions",
				"resource": string(resource),
				"action":   string(action),
			})
			return
		}
		c.Next()
	}
}
