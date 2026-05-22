package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/governance"
)

// GovernanceResourceExtractor returns the {service, env} pair for a request.
// Route-specific implementations live next to the handler — e.g.
// service.AppDeployGovernanceResource resolves the App and its Environment
// from the store.
//
// Returning ok=false means "this request doesn't have a deploy target the
// gate cares about" and the middleware permits the request without calling
// Grovernance. Use it to short-circuit, e.g. for noop pipelines.
type GovernanceResourceExtractor func(c *gin.Context) (service string, env string, ok bool, err error)

// BreakGlassOption tunes the optional break-glass branch. When Enabled is
// true AND governance is unreachable AND env is fail-closed AND the request
// carries a non-empty X-Break-Glass-Justification header, the middleware
// records a structured slog event tagged break_glass=true and lets the
// request through. Without all four conditions the existing 503 fires.
//
// Break-glass is a deliberately narrow escape hatch: it is not active when
// the gate is reachable (deny is deny), it is not active on a healthy
// fail-open env (no rescue needed), and it is not active without an explicit
// operator justification on the wire. The event is the audit trail.
type BreakGlassOption struct {
	Enabled bool
}

// RequireGovernanceAllow returns Gin middleware that consults Grovernance
// before letting the request through. On DENY it responds 403 with the reason
// from Grovernance. On a fail-closed transport error it responds 503 (or
// passes via break-glass if configured and an operator supplies a
// justification header).
//
// The middleware is a no-op when client.Enabled() is false (i.e. the operator
// has not configured COOKER_GOVERNANCE_URL).
func RequireGovernanceAllow(client *governance.Client, extract GovernanceResourceExtractor, opts ...BreakGlassOption) gin.HandlerFunc {
	var bg BreakGlassOption
	if len(opts) > 0 {
		bg = opts[0]
	}
	return func(c *gin.Context) {
		if client == nil || !client.Enabled() {
			c.Next()
			return
		}

		service, env, ok, err := extract(c)
		if err != nil {
			slog.Warn("governance: resource extractor failed",
				"path", c.Request.URL.Path, "err", err)
			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{
				"error": "governance pre-check failed",
			})
			return
		}
		if !ok {
			c.Next()
			return
		}

		token, ok := extractBearer(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required for governance check",
			})
			return
		}

		decision, err := client.Authorize(c.Request.Context(), token, service, env, requestID(c))
		if err != nil {
			if errors.Is(err, governance.ErrGovernanceUnreachable) {
				if just, ok := breakGlassJustification(c); ok && bg.Enabled {
					user := c.GetString("user_sub")
					slog.Warn("governance: break-glass invoked",
						"service", service, "env", env,
						"requester", user,
						"justification", just,
						"break_glass", true)
					c.Set("governance.break_glass", true)
					c.Set("governance.break_glass_justification", just)
					c.Next()
					return
				}
				slog.Warn("governance: fail-closed",
					"service", service, "env", env, "err", err)
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
					"error": "governance unreachable; deploy blocked (fail-closed)",
					"env":   env,
				})
				return
			}
			slog.Error("governance: authorize error",
				"service", service, "env", env, "err", err)
			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{
				"error": "governance call failed",
			})
			return
		}

		if !decision.Allowed() {
			if !decision.Enforced {
				// Advisory deny: gate would have blocked, but its mode for
				// this (service, env) is advisory. Log the would-have-blocked
				// signal so the bake-period dashboard captures it, then let
				// the request through. The catalog UI is the kill switch.
				slog.Info("governance: advisory deny (would have blocked)",
					"service", service, "env", env,
					"reason", decision.Reason, "policy", decision.PolicyID, "audit", decision.AuditID,
					"mode", decision.EnforcementMode)
				c.Set("governance.audit_id", decision.AuditID)
				c.Set("governance.policy_id", decision.PolicyID)
				c.Set("governance.advisory_deny", true)
				c.Next()
				return
			}
			slog.Info("governance: deny",
				"service", service, "env", env,
				"reason", decision.Reason, "policy", decision.PolicyID, "audit", decision.AuditID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":     "denied by governance",
				"reason":    decision.Reason,
				"policy_id": decision.PolicyID,
				"audit_id":  decision.AuditID,
				"service":   service,
				"env":       env,
			})
			return
		}

		// Stash the decision for the handler / downstream middleware to log.
		c.Set("governance.audit_id", decision.AuditID)
		c.Set("governance.policy_id", decision.PolicyID)
		c.Next()
	}
}

func extractBearer(c *gin.Context) (string, bool) {
	authz := c.GetHeader("Authorization")
	if authz == "" {
		return "", false
	}
	parts := strings.SplitN(authz, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", false
	}
	tok := strings.TrimSpace(parts[1])
	return tok, tok != ""
}

// breakGlassJustification returns a trimmed non-empty value of the
// X-Break-Glass-Justification header, or empty + false. Whitespace-only
// values are rejected so a copy-pasted accidental space cannot trigger
// break-glass.
func breakGlassJustification(c *gin.Context) (string, bool) {
	v := strings.TrimSpace(c.GetHeader("X-Break-Glass-Justification"))
	return v, v != ""
}

func requestID(c *gin.Context) string {
	if v := c.GetHeader("X-Request-ID"); v != "" {
		return v
	}
	if v, ok := c.Get("request_id"); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return c.GetString("RequestID")
}
