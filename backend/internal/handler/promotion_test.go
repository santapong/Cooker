package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
)

// seedRunAndEnvs sets up a pipeline, a run under it, and the given
// environments, returning the wired handler.
func seedPromotionFixture(t *testing.T, envs ...*model.Environment) *Handler {
	t.Helper()
	h := newTestHandler(t)
	ctx := context.Background()
	if err := h.Store.Pipelines.Create(ctx, &model.Pipeline{ID: "p1", Name: "p"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Runs.Create(ctx, &model.PipelineRun{ID: "r1", PipelineID: "p1"}); err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if err := h.Store.Environments.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

// TestPromotion_EndToEnd_PersistsAndSurfacesViaEnvStatus is the
// regression for HS26-05-01 + HS26-05-08: a promote writes a real row,
// and env-status reflects it instead of returning [].
func TestPromotion_EndToEnd_PersistsAndSurfacesViaEnvStatus(t *testing.T) {
	h := seedPromotionFixture(t, &model.Environment{
		ID: "staging", Name: "Staging", Order: 2,
		Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 1},
	})
	operator := &auth.Claims{Subject: "sub-op", Email: "op@x.com", Roles: []string{string(auth.RoleOperator)}}
	approver := &auth.Claims{Subject: "sub-ap", Email: "ap@x.com", Roles: []string{string(auth.RoleApprover)}}

	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/promote", withUser(h.PromoteRun, operator))
	r.POST("/pipelines/:id/runs/:runId/approve", withUser(h.ApprovePromotion, approver))
	r.GET("/pipelines/:id/runs/:runId/env-status", withUser(h.GetEnvStatus, operator))

	// 1. env-status before any promotion: real empty list.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pipelines/p1/runs/r1/env-status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("env-status: %d %s", w.Code, w.Body.String())
	}
	if statuses := decodeStatuses(t, w); len(statuses) != 0 {
		t.Fatalf("expected 0 statuses before promotion, got %d", len(statuses))
	}

	// 2. Promote (operator).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/promote", map[string]string{"environmentId": "staging"}))
	if w.Code != http.StatusOK {
		t.Fatalf("promote: %d %s", w.Code, w.Body.String())
	}

	// 3. env-status now reports the awaiting-approval gate (HS26-05-08).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pipelines/p1/runs/r1/env-status", nil))
	statuses := decodeStatuses(t, w)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status after promote, got %d", len(statuses))
	}
	if statuses[0].EnvironmentID != "staging" || statuses[0].Status != model.EnvStatus(model.PromotionAwaitingApproval) {
		t.Errorf("unexpected status: %+v", statuses[0])
	}

	// 4. Approve (approver) → gate advances, persisted.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/approve", map[string]string{"environmentId": "staging"}))
	if w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}

	// 5. env-status reflects the approved gate — proof it persisted, not
	// the old theatre that reset on refresh (HS26-05-01).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pipelines/p1/runs/r1/env-status", nil))
	statuses = decodeStatuses(t, w)
	if len(statuses) != 1 || statuses[0].Status != model.EnvStatus(model.PromotionApproved) {
		t.Fatalf("expected staging approved after refresh, got %+v", statuses)
	}
}

// TestPromote_RequiresEnvironmentId locks the body contract: promote
// without environmentId is a 400, not a silent no-op.
func TestPromote_RequiresEnvironmentId(t *testing.T) {
	h := seedPromotionFixture(t)
	operator := &auth.Claims{Subject: "s", Email: "op@x.com", Roles: []string{string(auth.RoleOperator)}}
	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/promote", withUser(h.PromoteRun, operator))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/promote", map[string]string{}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without environmentId, got %d: %s", w.Code, w.Body.String())
	}
}

// TestApprove_OperatorForbidden confirms the approver-role gate: an
// operator can promote but must NOT be able to approve.
func TestApprove_OperatorForbidden(t *testing.T) {
	h := seedPromotionFixture(t, &model.Environment{
		ID: "staging", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 1},
	})
	operator := &auth.Claims{Subject: "s", Email: "op@x.com", Roles: []string{string(auth.RoleOperator)}}
	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/approve", withUser(h.ApprovePromotion, operator))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/approve", map[string]string{"environmentId": "staging"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("operator must not approve: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestApprove_AdminAllowed confirms admin satisfies the approver gate.
func TestApprove_AdminAllowed(t *testing.T) {
	h := seedPromotionFixture(t, &model.Environment{
		ID: "staging", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 1},
	})
	admin := &auth.Claims{Subject: "sub-admin", Email: "admin@x.com", Roles: []string{string(auth.RoleAdmin)}}
	if _, err := h.Promotions.Promote(context.Background(), "r1", "p1", "staging", "op@x.com"); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/approve", withUser(h.ApprovePromotion, admin))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/approve", map[string]string{"environmentId": "staging", "note": "ok"}))
	if w.Code != http.StatusOK {
		t.Fatalf("admin should approve: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPromote_AutoStrategyDeploysImmediately covers the auto
// short-circuit path through the handler.
func TestPromote_AutoStrategyDeploysImmediately(t *testing.T) {
	h := seedPromotionFixture(t, &model.Environment{
		ID: "dev", Promotion: model.PromotionPolicy{Strategy: "auto"},
	})
	operator := &auth.Claims{Subject: "s", Email: "op@x.com", Roles: []string{string(auth.RoleOperator)}}
	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/promote", withUser(h.PromoteRun, operator))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/promote", map[string]string{"environmentId": "dev"}))
	if w.Code != http.StatusOK {
		t.Fatalf("promote auto: %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != string(model.PromotionDeployed) {
		t.Errorf("auto promotion should be deployed immediately, got %v", body["status"])
	}
}

func decodeStatuses(t *testing.T, w *httptest.ResponseRecorder) []model.EnvironmentStatus {
	t.Helper()
	var body struct {
		Statuses []model.EnvironmentStatus `json:"statuses"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode env-status: %v (body=%s)", err, w.Body.String())
	}
	return body.Statuses
}
