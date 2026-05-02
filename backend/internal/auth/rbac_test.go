package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func TestCanApprovePromotion_Admin(t *testing.T) {
	claims := &Claims{Roles: []string{string(RoleAdmin)}}
	if !CanApprovePromotion(claims) {
		t.Error("admin should be able to approve promotions")
	}
}

func TestCanApprovePromotion_Approver(t *testing.T) {
	claims := &Claims{Roles: []string{string(RoleApprover)}}
	if !CanApprovePromotion(claims) {
		t.Error("approver should be able to approve promotions")
	}
}

func TestCanApprovePromotion_Operator(t *testing.T) {
	// Operators can run pipelines but not rubber-stamp promotions —
	// the dedicated approver role exists precisely to separate those
	// duties.
	claims := &Claims{Roles: []string{string(RoleOperator)}}
	if CanApprovePromotion(claims) {
		t.Error("operator alone should NOT be able to approve promotions")
	}
}

func TestCanApprovePromotion_Viewer(t *testing.T) {
	claims := &Claims{Roles: []string{string(RoleViewer)}}
	if CanApprovePromotion(claims) {
		t.Error("viewer should not be able to approve promotions")
	}
}

func TestCanApprovePromotion_MultipleRoles(t *testing.T) {
	claims := &Claims{Roles: []string{string(RoleViewer), string(RoleApprover)}}
	if !CanApprovePromotion(claims) {
		t.Error("user with approver role should be able to approve")
	}
}

func TestCanRevealSecret(t *testing.T) {
	if !CanRevealSecret(&Claims{Roles: []string{string(RoleAdmin)}}) {
		t.Error("admin should reveal secrets")
	}
	if CanRevealSecret(&Claims{Roles: []string{string(RoleApprover)}}) {
		t.Error("approver should NOT reveal secrets")
	}
	if CanRevealSecret(&Claims{Roles: []string{string(RoleOperator)}}) {
		t.Error("operator should NOT reveal secrets")
	}
	if CanRevealSecret(nil) {
		t.Error("nil claims must not authorize")
	}
}

func TestCanApprovePromotion_NoRoles(t *testing.T) {
	claims := &Claims{Roles: []string{}}
	if CanApprovePromotion(claims) {
		t.Error("user with no roles should not be able to approve")
	}
}

func TestMapGroupsToRoles_AdminGroup(t *testing.T) {
	roles := MapGroupsToRoles([]string{"cooker-admins"})
	if len(roles) != 1 || roles[0] != string(RoleAdmin) {
		t.Errorf("expected [admin], got %v", roles)
	}
}

func TestMapGroupsToRoles_OperatorGroup(t *testing.T) {
	roles := MapGroupsToRoles([]string{"cooker-operators"})
	if len(roles) != 1 || roles[0] != string(RoleOperator) {
		t.Errorf("expected [operator], got %v", roles)
	}
}

func TestMapGroupsToRoles_ViewerGroup(t *testing.T) {
	roles := MapGroupsToRoles([]string{"cooker-viewers"})
	if len(roles) != 1 || roles[0] != string(RoleViewer) {
		t.Errorf("expected [viewer], got %v", roles)
	}
}

func TestMapGroupsToRoles_MultipleGroups(t *testing.T) {
	roles := MapGroupsToRoles([]string{"cooker-admins", "cooker-operators"})
	if len(roles) != 2 {
		t.Errorf("expected 2 roles, got %d: %v", len(roles), roles)
	}
}

func TestMapGroupsToRoles_DuplicateGroups(t *testing.T) {
	roles := MapGroupsToRoles([]string{"cooker-admins", "cooker-admins"})
	if len(roles) != 1 {
		t.Errorf("expected 1 role (deduped), got %d: %v", len(roles), roles)
	}
}

func TestMapGroupsToRoles_UnknownGroups(t *testing.T) {
	roles := MapGroupsToRoles([]string{"unknown-group", "another-group"})
	if len(roles) != 1 || roles[0] != string(RoleViewer) {
		t.Errorf("expected default [viewer], got %v", roles)
	}
}

func TestMapGroupsToRoles_EmptyGroups(t *testing.T) {
	roles := MapGroupsToRoles([]string{})
	if len(roles) != 1 || roles[0] != string(RoleViewer) {
		t.Errorf("expected default [viewer], got %v", roles)
	}
}

func TestMapGroupsToRoles_MixedGroups(t *testing.T) {
	roles := MapGroupsToRoles([]string{"cooker-admins", "unknown-group", "cooker-viewers"})
	if len(roles) != 2 {
		t.Errorf("expected 2 roles, got %d: %v", len(roles), roles)
	}
}

func TestMapGroupsToRolesWith_CustomMap(t *testing.T) {
	custom := map[string]string{
		"platform-admins": string(RoleAdmin),
		"platform-eng":    string(RoleOperator),
	}
	roles := MapGroupsToRolesWith([]string{"platform-admins"}, custom)
	if len(roles) != 1 || roles[0] != string(RoleAdmin) {
		t.Errorf("expected [admin], got %v", roles)
	}
	// Default groups should NOT match when a custom map is supplied.
	roles = MapGroupsToRolesWith([]string{"cooker-admins"}, custom)
	if len(roles) != 1 || roles[0] != string(RoleViewer) {
		t.Errorf("expected default [viewer] for unmapped group, got %v", roles)
	}
}

func TestMapGroupsToRolesWith_NilFallsBackToDefault(t *testing.T) {
	roles := MapGroupsToRolesWith([]string{"cooker-admins"}, nil)
	if len(roles) != 1 || roles[0] != string(RoleAdmin) {
		t.Errorf("expected [admin] from default map, got %v", roles)
	}
}

func TestMapGroupsToRolesWith_EmptyFallsBackToDefault(t *testing.T) {
	roles := MapGroupsToRolesWith([]string{"cooker-operators"}, map[string]string{})
	if len(roles) != 1 || roles[0] != string(RoleOperator) {
		t.Errorf("expected [operator] from default map, got %v", roles)
	}
}

// serveWithClaims builds a Gin engine with the given claims injected
// into every request, then registers a route guarded by the given
// RequireRole middleware. Used by the middleware integration tests
// below to exercise the full request pipeline.
func serveWithClaims(claims *Claims, required ...Role) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if claims != nil {
			c.Set("user", claims)
		}
		c.Next()
	})
	r.GET("/protected", RequireRole(required...), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func TestRequireRole_NoClaims_401(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaims(nil, RoleOperator, RoleAdmin).ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireRole_Viewer_403(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaims(&Claims{Roles: []string{string(RoleViewer)}}, RoleOperator, RoleAdmin).ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for viewer, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireRole_Approver_403_OnOperatorGate(t *testing.T) {
	// A dedicated approver should NOT satisfy an operator-or-admin
	// gate — approvers only gain permission on approval endpoints
	// via the separate CanApprovePromotion check.
	w := httptest.NewRecorder()
	serveWithClaims(&Claims{Roles: []string{string(RoleApprover)}}, RoleOperator, RoleAdmin).ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for approver on operator gate, got %d", w.Code)
	}
}

func TestRequireRole_Operator_200(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaims(&Claims{Roles: []string{string(RoleOperator)}}, RoleOperator, RoleAdmin).ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for operator, got %d", w.Code)
	}
}

func TestRequireRole_Admin_200_OnAdminOnly(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaims(&Claims{Roles: []string{string(RoleAdmin)}}, RoleAdmin).ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin on admin-only gate, got %d", w.Code)
	}
}

// serveWithClaimsMFA is the RequireMFA equivalent of serveWithClaims.
func serveWithClaimsMFA(claims *Claims, acrValues ...string) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if claims != nil {
			c.Set("user", claims)
		}
		c.Next()
	})
	r.GET("/protected", RequireMFA(acrValues...), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func TestRequireMFA_DisabledWhenNoACR(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaimsMFA(&Claims{Roles: []string{string(RoleAdmin)}}).ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when no ACR values configured, got %d", w.Code)
	}
}

func TestRequireMFA_Allows_MatchingACR(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaimsMFA(&Claims{ACR: "mfa"}, "mfa").ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for matching ACR, got %d", w.Code)
	}
}

func TestRequireMFA_Allows_MatchingAMR(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaimsMFA(&Claims{AMR: []string{"pwd", "otp"}}, "otp").ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for matching AMR, got %d", w.Code)
	}
}

func TestRequireMFA_Rejects_MissingACR(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaimsMFA(&Claims{ACR: "loa1"}, "mfa").ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-matching ACR, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "mfa_required") {
		t.Errorf("response should include mfa_required, got: %s", w.Body.String())
	}
}

func TestRequireMFA_Unauthenticated_401(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaimsMFA(nil, "mfa").ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no user, got %d", w.Code)
	}
}

func TestRequireRole_Operator_403_OnAdminOnly(t *testing.T) {
	w := httptest.NewRecorder()
	serveWithClaims(&Claims{Roles: []string{string(RoleOperator)}}, RoleAdmin).ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/protected", nil),
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for operator on admin-only gate, got %d", w.Code)
	}
}
