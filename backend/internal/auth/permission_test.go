package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func TestPermissionNilClaimsDenies(t *testing.T) {
	if Permission(nil, ResourcePipeline, ActionRead) {
		t.Fatal("nil claims should never pass")
	}
}

func TestPermissionUndeclaredPairDenies(t *testing.T) {
	// A made-up resource that's not in the matrix — deny even for
	// an admin. Matches Dokploy's allow-list-by-default posture.
	claims := &Claims{Roles: []string{string(RoleAdmin)}}
	if Permission(claims, Resource("made-up"), ActionRead) {
		t.Fatal("undeclared resource should deny")
	}
}

func TestPermissionMatrix(t *testing.T) {
	cases := []struct {
		role     Role
		resource Resource
		action   Action
		allow    bool
	}{
		// Viewer can read but not write.
		{RoleViewer, ResourcePipeline, ActionRead, true},
		{RoleViewer, ResourcePipeline, ActionCreate, false},
		{RoleViewer, ResourcePipeline, ActionInvoke, false},
		{RoleViewer, ResourceSecret, ActionReveal, false},
		// Operator can create + invoke but not delete or reveal.
		{RoleOperator, ResourcePipeline, ActionCreate, true},
		{RoleOperator, ResourcePipeline, ActionInvoke, true},
		{RoleOperator, ResourcePipeline, ActionDelete, false},
		{RoleOperator, ResourceSecret, ActionReveal, false},
		{RoleOperator, ResourceSecret, ActionUpdate, false},
		// Approver can approve promotions but not invoke them.
		{RoleApprover, ResourcePromotion, ActionApprove, true},
		{RoleApprover, ResourcePromotion, ActionInvoke, false},
		// Admin can do everything.
		{RoleAdmin, ResourceSecret, ActionReveal, true},
		{RoleAdmin, ResourceSecret, ActionUpdate, true},
		{RoleAdmin, ResourceSecret, ActionDelete, true},
		{RoleAdmin, ResourcePipeline, ActionDelete, true},
		{RoleAdmin, ResourceRegistry, ActionCreate, true},
		{RoleAdmin, ResourceWebhook, ActionUpdate, true},
	}
	for _, c := range cases {
		claims := &Claims{Roles: []string{string(c.role)}}
		got := Permission(claims, c.resource, c.action)
		if got != c.allow {
			t.Errorf("%s + %s/%s = %v want %v", c.role, c.resource, c.action, got, c.allow)
		}
	}
}

func TestPermissionMultipleRolesUnion(t *testing.T) {
	// A user with multiple roles inherits the union. Real OIDC
	// configurations frequently grant operator+approver to release
	// engineers.
	claims := &Claims{Roles: []string{string(RoleOperator), string(RoleApprover)}}
	if !Permission(claims, ResourcePipeline, ActionInvoke) {
		t.Fatal("operator+approver should be able to Invoke pipeline")
	}
	if !Permission(claims, ResourcePromotion, ActionApprove) {
		t.Fatal("operator+approver should be able to Approve promotion")
	}
}

func TestRequirePermissionAllowsAdminOnSecretReveal(t *testing.T) {
	r := gin.New()
	r.GET("/secret",
		func(c *gin.Context) {
			c.Set("user", &Claims{Subject: "u", Roles: []string{string(RoleAdmin)}})
			c.Next()
		},
		RequirePermission(ResourceSecret, ActionReveal),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) },
	)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secret", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestRequirePermissionDeniesOperatorOnSecretReveal(t *testing.T) {
	r := gin.New()
	r.GET("/secret",
		func(c *gin.Context) {
			c.Set("user", &Claims{Subject: "u", Roles: []string{string(RoleOperator)}})
			c.Next()
		},
		RequirePermission(ResourceSecret, ActionReveal),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) },
	)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secret", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d want 403", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["resource"] != "secret" || body["action"] != "reveal" {
		t.Fatalf("403 body missing resource/action context: %v", body)
	}
}

func TestRequirePermissionUnauthorizedWithoutClaims(t *testing.T) {
	r := gin.New()
	r.GET("/p",
		RequirePermission(ResourcePipeline, ActionRead),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) },
	)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d want 401", w.Code)
	}
}
