package auth

import "testing"

func TestCanApprovePromotion_Admin(t *testing.T) {
	claims := &Claims{Roles: []string{string(RoleAdmin)}}
	if !CanApprovePromotion(claims) {
		t.Error("admin should be able to approve promotions")
	}
}

func TestCanApprovePromotion_Operator(t *testing.T) {
	claims := &Claims{Roles: []string{string(RoleOperator)}}
	if !CanApprovePromotion(claims) {
		t.Error("operator should be able to approve promotions")
	}
}

func TestCanApprovePromotion_Viewer(t *testing.T) {
	claims := &Claims{Roles: []string{string(RoleViewer)}}
	if CanApprovePromotion(claims) {
		t.Error("viewer should not be able to approve promotions")
	}
}

func TestCanApprovePromotion_MultipleRoles(t *testing.T) {
	claims := &Claims{Roles: []string{string(RoleViewer), string(RoleOperator)}}
	if !CanApprovePromotion(claims) {
		t.Error("user with operator role should be able to approve")
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
