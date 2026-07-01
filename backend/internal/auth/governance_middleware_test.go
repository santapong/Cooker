package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/governance"
)

func init() { gin.SetMode(gin.TestMode) }

func fixedExtractor(svc, env string) auth.GovernanceResourceExtractor {
	return func(c *gin.Context) (string, string, bool, error) { return svc, env, true, nil }
}

func TestMiddleware_NoOpWhenDisabled(t *testing.T) {
	client := governance.New("", nil, nil) // disabled
	r := gin.New()
	r.POST("/deploy", auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod")), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (middleware should be no-op)", w.Code)
	}
}

func TestMiddleware_AllowsWhenGrovernanceAllows(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(governance.Decision{Decision: "allow", AuditID: "audit-1"})
	}))
	defer upstream.Close()

	client := governance.New(upstream.URL, nil, nil)
	r := gin.New()
	r.POST("/deploy", auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod")), func(c *gin.Context) {
		c.JSON(200, gin.H{"audit_id": c.GetString("governance.audit_id")})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-alice")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "audit-1") {
		t.Errorf("audit id not propagated: %s", w.Body.String())
	}
}

// TestMiddleware_ForwardsBreakGlassJustificationAsABAC asserts the middleware
// plumbs the X-Break-Glass-Justification header into the outbound /authorize
// context.break_glass field (X-abac-context) when the gate is REACHABLE — so
// the gate's Cedar policy can factor break-glass into the verdict, rather than
// the header being silently discarded.
func TestMiddleware_ForwardsBreakGlassJustificationAsABAC(t *testing.T) {
	var gotBreakGlass *bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Context struct {
				BreakGlass *bool `json:"break_glass"`
			} `json:"context"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotBreakGlass = body.Context.BreakGlass
		_ = json.NewEncoder(w).Encode(governance.Decision{Decision: "allow"})
	}))
	defer upstream.Close()

	client := governance.New(upstream.URL, nil, nil)
	r := gin.New()
	r.POST("/deploy",
		auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod"),
			auth.BreakGlassOption{Enabled: true}),
		func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-alice")
	req.Header.Set("X-Break-Glass-Justification", "INC-42 hot patch")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if gotBreakGlass == nil || *gotBreakGlass != true {
		t.Errorf("outbound context.break_glass = %v, want true", gotBreakGlass)
	}
}

// TestMiddleware_NoBreakGlassHeader_OmitsABAC asserts the outbound request
// carries no break_glass field when no justification header is present.
func TestMiddleware_NoBreakGlassHeader_OmitsABAC(t *testing.T) {
	var sawBreakGlass bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]any
		_ = json.NewDecoder(r.Body).Decode(&raw)
		if ctx, ok := raw["context"].(map[string]any); ok {
			_, sawBreakGlass = ctx["break_glass"]
		}
		_ = json.NewEncoder(w).Encode(governance.Decision{Decision: "allow"})
	}))
	defer upstream.Close()

	client := governance.New(upstream.URL, nil, nil)
	r := gin.New()
	r.POST("/deploy",
		auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod")),
		func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-alice")
	r.ServeHTTP(w, req)

	if sawBreakGlass {
		t.Error("break_glass must be omitted when no justification header is present")
	}
}

func TestMiddleware_403OnDeny(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(governance.Decision{
			Decision:        "deny",
			Reason:          "actor not in prod-deployer group for svc-x",
			PolicyID:        "rule.prod.human",
			AuditID:         "audit-2",
			Enforced:        true,
			EnforcementMode: "enforce",
		})
	}))
	defer upstream.Close()

	client := governance.New(upstream.URL, nil, nil)
	r := gin.New()
	r.POST("/deploy", auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod")), func(c *gin.Context) {
		c.JSON(200, gin.H{"unreachable": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-bob")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "prod-deployer group") {
		t.Errorf("reason not in body: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "audit-2") {
		t.Errorf("audit id not in body: %s", w.Body.String())
	}
}

// TestMiddleware_AdvisoryDeny_PassesAndLogs is the Milestone-B keystone
// behaviour on the Cooker side: when the gate returns deny but enforced=false
// (advisory mode for that service/env), the middleware logs the would-have-
// blocked event and lets the request through. The advisory_deny flag is set
// on the gin context for downstream audit middleware.
func TestMiddleware_AdvisoryDeny_PassesAndLogs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(governance.Decision{
			Decision:        "deny",
			Reason:          "actor not in prod-deployer group for svc-x",
			PolicyID:        "rule.prod.human",
			AuditID:         "audit-3",
			Enforced:        false,
			EnforcementMode: "advisory",
		})
	}))
	defer upstream.Close()

	client := governance.New(upstream.URL, nil, nil)
	var sawAdvisoryDeny bool
	r := gin.New()
	r.POST("/deploy", auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod")), func(c *gin.Context) {
		sawAdvisoryDeny = c.GetBool("governance.advisory_deny")
		c.JSON(200, gin.H{
			"audit_id":      c.GetString("governance.audit_id"),
			"advisory_deny": c.GetBool("governance.advisory_deny"),
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-bob")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (advisory deny should not block)", w.Code)
	}
	if !sawAdvisoryDeny {
		t.Error("governance.advisory_deny flag was not set on the gin context")
	}
	if !strings.Contains(w.Body.String(), "audit-3") {
		t.Errorf("audit id not propagated: %s", w.Body.String())
	}
}

func TestMiddleware_503OnFailClosed(t *testing.T) {
	client := governance.New("http://127.0.0.1:1/not-listening", nil, []string{"dev"})
	r := gin.New()
	r.POST("/deploy", auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod")), func(c *gin.Context) {
		c.JSON(200, gin.H{"unreachable": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-alice")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestMiddleware_FailOpenLetsThrough(t *testing.T) {
	client := governance.New("http://127.0.0.1:1/not-listening", nil, []string{"dev"})
	r := gin.New()
	r.POST("/deploy", auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "dev")), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-alice")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (dev is fail-open)", w.Code)
	}
}

func TestMiddleware_401WhenNoBearer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(governance.Decision{Decision: "allow"})
	}))
	defer upstream.Close()

	client := governance.New(upstream.URL, nil, nil)
	r := gin.New()
	r.POST("/deploy", auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod")), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestMiddleware_BreakGlass_PassesWhenAllConditionsMet(t *testing.T) {
	// Governance unreachable + env not in fail-open + break-glass enabled +
	// justification header present → log + pass.
	client := governance.New("http://127.0.0.1:1/not-listening", nil, []string{"dev"})
	var sawBreakGlass bool
	r := gin.New()
	r.POST("/deploy",
		auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod"),
			auth.BreakGlassOption{Enabled: true}),
		func(c *gin.Context) {
			sawBreakGlass = c.GetBool("governance.break_glass")
			c.JSON(200, gin.H{"break_glass": c.GetBool("governance.break_glass")})
		})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-alice")
	req.Header.Set("X-Break-Glass-Justification", "INC-42 root cause hot patch")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (break-glass should pass)", w.Code)
	}
	if !sawBreakGlass {
		t.Error("governance.break_glass flag was not set")
	}
}

func TestMiddleware_BreakGlass_NoOpWithoutJustification(t *testing.T) {
	// Break-glass enabled but no header → 503 (fail-closed).
	client := governance.New("http://127.0.0.1:1/not-listening", nil, []string{"dev"})
	r := gin.New()
	r.POST("/deploy",
		auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod"),
			auth.BreakGlassOption{Enabled: true}),
		func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-alice")
	// No X-Break-Glass-Justification header.
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (no header, no break-glass)", w.Code)
	}
}

func TestMiddleware_BreakGlass_NoOpWhenDisabled(t *testing.T) {
	// Header present but break-glass flag off → 503 (fail-closed).
	client := governance.New("http://127.0.0.1:1/not-listening", nil, []string{"dev"})
	r := gin.New()
	r.POST("/deploy",
		auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod"),
			auth.BreakGlassOption{Enabled: false}),
		func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-alice")
	req.Header.Set("X-Break-Glass-Justification", "INC-42")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (flag off ignores header)", w.Code)
	}
}

func TestMiddleware_BreakGlass_InactiveWhenGateReachable(t *testing.T) {
	// Gate returns a clean deny + enforced=true. Justification header present
	// but governance is REACHABLE, so break-glass MUST NOT fire — deny is deny.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(governance.Decision{
			Decision: "deny", Reason: "not allowed", PolicyID: "rule.prod.human",
			AuditID: "audit-x", Enforced: true,
		})
	}))
	defer upstream.Close()

	client := governance.New(upstream.URL, nil, nil)
	r := gin.New()
	r.POST("/deploy",
		auth.RequireGovernanceAllow(client, fixedExtractor("svc-x", "prod"),
			auth.BreakGlassOption{Enabled: true}),
		func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-bob")
	req.Header.Set("X-Break-Glass-Justification", "INC-42")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (break-glass MUST NOT bypass a live deny)", w.Code)
	}
}

func TestMiddleware_SkipsWhenExtractorReturnsOkFalse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called when extractor returns ok=false")
	}))
	defer upstream.Close()

	skip := func(c *gin.Context) (string, string, bool, error) { return "", "", false, nil }

	client := governance.New(upstream.URL, nil, nil)
	r := gin.New()
	r.POST("/deploy", auth.RequireGovernanceAllow(client, skip), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-alice")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (extractor skipped)", w.Code)
	}
}
