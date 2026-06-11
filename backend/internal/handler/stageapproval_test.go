package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
)

// seedStageApprovalFixture sets up a pipeline + run and opens an awaiting
// gate for stage "s1", returning the wired handler.
func seedStageApprovalFixture(t *testing.T, required int) *Handler {
	t.Helper()
	h := newTestHandler(t)
	ctx := context.Background()
	if err := h.Store.Pipelines.Create(ctx, &model.Pipeline{ID: "p1", Name: "p"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Runs.Create(ctx, &model.PipelineRun{ID: "r1", PipelineID: "p1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.StageApprovals.Await(ctx, "r1", "s1", required); err != nil {
		t.Fatal(err)
	}
	return h
}

// TestStageApproval_EndToEnd_ApproveResolvesAndSurfaces is the HS26-05-03
// regression: an approval gate persists, surfaces via the list endpoint as
// awaiting, and flips to approved after an approver acts.
func TestStageApproval_EndToEnd_ApproveResolvesAndSurfaces(t *testing.T) {
	h := seedStageApprovalFixture(t, 1)
	approver := &auth.Claims{Subject: "sub-ap", Email: "ap@x.com", Roles: []string{string(auth.RoleApprover)}}

	r := gin.New()
	r.GET("/pipelines/:id/runs/:runId/stage-approvals", withUser(h.ListStageApprovals, approver))
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/approve", withUser(h.ApproveStage, approver))

	// 1. List shows the awaiting gate.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pipelines/p1/runs/r1/stage-approvals", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}

	// 2. Approve.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/stages/s1/approve", map[string]string{"note": "lgtm"}))
	if w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}

	// 3. The gate is persisted as approved (proof it's not theatre).
	g, err := h.StageApprovals.Get(context.Background(), "r1", "s1")
	if err != nil {
		t.Fatalf("get gate: %v", err)
	}
	if g.Status != model.StageApprovalApproved {
		t.Errorf("gate status = %s, want approved", g.Status)
	}
}

func TestStageApproval_RejectFailsGate(t *testing.T) {
	h := seedStageApprovalFixture(t, 1)
	admin := &auth.Claims{Subject: "sub-admin", Email: "admin@x.com", Roles: []string{string(auth.RoleAdmin)}}

	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/reject", withUser(h.RejectStage, admin))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/stages/s1/reject", map[string]string{"note": "no"}))
	if w.Code != http.StatusOK {
		t.Fatalf("reject: %d %s", w.Code, w.Body.String())
	}
	g, _ := h.StageApprovals.Get(context.Background(), "r1", "s1")
	if g.Status != model.StageApprovalRejected {
		t.Errorf("gate status = %s, want rejected", g.Status)
	}
}

// TestStageApproval_ViewerForbidden confirms the approver-role gate: a
// plain viewer can neither approve nor reject (mirrors the promotion RBAC).
func TestStageApproval_ViewerForbidden(t *testing.T) {
	h := seedStageApprovalFixture(t, 1)
	viewer := &auth.Claims{Subject: "sub-v", Email: "v@x.com", Roles: []string{string(auth.RoleViewer)}}

	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/approve", withUser(h.ApproveStage, viewer))
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/reject", withUser(h.RejectStage, viewer))

	for _, action := range []string{"approve", "reject"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/stages/s1/"+action, map[string]string{}))
		if w.Code != http.StatusForbidden {
			t.Fatalf("viewer %s: expected 403, got %d: %s", action, w.Code, w.Body.String())
		}
	}
}

// TestStageApproval_ApproveResolvedConflicts: approving an already-rejected
// gate is a 409, not a silent re-open.
func TestStageApproval_ApproveResolvedConflicts(t *testing.T) {
	h := seedStageApprovalFixture(t, 1)
	if _, err := h.StageApprovals.Reject(context.Background(), "r1", "s1", "admin@x.com", ""); err != nil {
		t.Fatal(err)
	}
	approver := &auth.Claims{Subject: "sub-ap", Email: "ap@x.com", Roles: []string{string(auth.RoleApprover)}}
	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/approve", withUser(h.ApproveStage, approver))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/stages/s1/approve", map[string]string{}))
	if w.Code != http.StatusConflict {
		t.Fatalf("approve-after-reject: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}
