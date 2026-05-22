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

// RequireGovernanceAllow returns Gin middleware that consults Grovernance
// before letting the request through. On DENY it responds 403 with the reason
// from Grovernance. On a fail-closed transport error it responds 503.
//
// The middleware is a no-op when client.Enabled() is false (i.e. the operator
// has not configured COOKER_GOVERNANCE_URL).
func RequireGovernanceAllow(client *governance.Client, extract GovernanceResourceExtractor) gin.HandlerFunc {
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
