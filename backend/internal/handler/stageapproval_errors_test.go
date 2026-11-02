package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

type unavailableApprovalStore struct{ store.StageApprovalStore }

func (*unavailableApprovalStore) GetGate(context.Context, string, string) (*model.StageApproval, error) {
	return nil, errors.New("synthetic-approval-storage-canary")
}

func (*unavailableApprovalStore) ListGates(context.Context, string) ([]*model.StageApproval, error) {
	return nil, errors.New("synthetic-approval-storage-canary")
}

func TestStageApprovalStorageErrorsDoNotExposeDetails(t *testing.T) {
	h := seedStageApprovalFixture(t, 1)
	h.StageApprovals = service.NewStageApprovalService(&unavailableApprovalStore{h.Store.StageApprovals})
	approver := &auth.Claims{Subject: "owned-approver", Email: "owned@example.invalid", Roles: []string{string(auth.RoleApprover)}}
	r := gin.New()
	r.GET("/pipelines/:id/runs/:runId/stage-approvals", withUser(h.ListStageApprovals, approver))
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/approve", withUser(h.ApproveStage, approver))
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/reject", withUser(h.RejectStage, approver))
	for _, action := range []string{"list", "approve", "reject"} {
		t.Run(action, func(t *testing.T) {
			method, path := http.MethodPost, "/pipelines/p1/runs/r1/stages/s1/"+action
			if action == "list" {
				method, path = http.MethodGet, "/pipelines/p1/runs/r1/stage-approvals"
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "synthetic-approval-storage-canary") {
				t.Fatalf("storage failure status/details: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestStageApprovalResponseDistinguishesVoteFromQuorum(t *testing.T) {
	h := seedStageApprovalFixture(t, 2)
	r := gin.New()
	var current *auth.Claims
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/approve", func(c *gin.Context) {
		withUser(h.ApproveStage, current)(c)
	})
	for i := 1; i <= 2; i++ {
		current = &auth.Claims{Subject: string(rune('a' + i)), Email: "owned@example.invalid", Roles: []string{string(auth.RoleApprover)}}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/stages/s1/approve", map[string]string{}))
		var body struct {
			Status model.StageApprovalStatus `json:"status"`
			Have   int                       `json:"approvalsHave"`
			Need   int                       `json:"approvalsNeed"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusOK || body.Have != i || body.Need != 2 {
			t.Fatalf("quorum response: %d %+v err=%v", w.Code, body, err)
		}
		want := model.StageApprovalAwaiting
		if i == 2 {
			want = model.StageApprovalApproved
		}
		if body.Status != want {
			t.Fatalf("vote count implied the wrong terminal state: %+v", body)
		}
	}
}

type stageApprovalRunLookupStore struct {
	store.RunStore
	err   error
	calls int
}

func (s *stageApprovalRunLookupStore) Get(ctx context.Context, id string) (*model.PipelineRun, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.RunStore.Get(ctx, id)
}

func stageApprovalLookupRouter(h *Handler) *gin.Engine {
	claims := &auth.Claims{Subject: "owned-approver", Email: "owned@example.invalid", Roles: []string{string(auth.RoleApprover)}}
	r := gin.New()
	r.GET("/pipelines/:id/runs/:runId/stage-approvals", withUser(h.ListStageApprovals, claims))
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/approve", withUser(h.ApproveStage, claims))
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/reject", withUser(h.RejectStage, claims))
	return r
}

func stageApprovalLookupRequest(action, pipelineID, runID string) *http.Request {
	if action == "list" {
		return httptest.NewRequest(http.MethodGet, "/pipelines/"+pipelineID+"/runs/"+runID+"/stage-approvals", nil)
	}
	return newRequest(http.MethodPost, "/pipelines/"+pipelineID+"/runs/"+runID+"/stages/s1/"+action, map[string]string{"note": "owned review"})
}

func assertStageApprovalLookupNoMutation(t *testing.T, h *Handler) {
	t.Helper()
	gate, err := h.Store.StageApprovals.GetGate(context.Background(), "r1", "s1")
	if err != nil || gate.Status != model.StageApprovalAwaiting || gate.ApprovalCount() != 0 || gate.ResolvedBy != "" || gate.ResolvedAt != nil {
		t.Fatalf("failed or mismatched run lookup changed the gate: %+v err=%v", gate, err)
	}
}

func TestStageApprovalRunLookupErrorsAreSafe(t *testing.T) {
	const canary = "synthetic-approval-run-storage-canary"
	for _, action := range []string{"list", "approve", "reject"} {
		for _, tc := range []struct {
			name    string
			cause   error
			status  int
			message string
		}{
			{"unavailable", errors.New("storage unavailable"), http.StatusInternalServerError, "approval run could not be loaded"},
			{"cancelled", context.Canceled, http.StatusInternalServerError, "approval run could not be loaded"},
			{"deadline", context.DeadlineExceeded, http.StatusInternalServerError, "approval run could not be loaded"},
			{"not-found", store.ErrNotFound, http.StatusNotFound, "run not found"},
			{"conflict", store.ErrConflict, http.StatusConflict, "version conflict; refetch and retry"},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				h := seedStageApprovalFixture(t, 1)
				lookup := &stageApprovalRunLookupStore{RunStore: h.Store.Runs, err: fmt.Errorf("%s: %w", canary, tc.cause)}
				h.Store.Runs = lookup
				w := httptest.NewRecorder()
				stageApprovalLookupRouter(h).ServeHTTP(w, stageApprovalLookupRequest(action, "p1", "r1"))
				var body struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != tc.status || body.Error != tc.message || strings.Contains(w.Body.String(), canary) {
					t.Errorf("run lookup failure status/details: %d %s err=%v", w.Code, w.Body.String(), err)
				}
				if lookup.calls != 1 {
					t.Errorf("run lookup calls=%d, want 1", lookup.calls)
				}
				assertStageApprovalLookupNoMutation(t, h)
			})
		}
	}
}

func TestStageApprovalRunLookupPreservesOwnershipNotFound(t *testing.T) {
	for _, action := range []string{"list", "approve", "reject"} {
		for _, tc := range []struct {
			name, pipelineID, runID string
		}{
			{"missing-run", "p1", "absent"},
			{"other-pipeline", "different", "r1"},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				h := seedStageApprovalFixture(t, 1)
				lookup := &stageApprovalRunLookupStore{RunStore: h.Store.Runs}
				h.Store.Runs = lookup
				w := httptest.NewRecorder()
				stageApprovalLookupRouter(h).ServeHTTP(w, stageApprovalLookupRequest(action, tc.pipelineID, tc.runID))
				var body struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusNotFound || body.Error != "run not found" {
					t.Fatalf("ownership or absent run must return the same 404: %d %s err=%v", w.Code, w.Body.String(), err)
				}
				if lookup.calls != 1 {
					t.Errorf("run lookup calls=%d, want 1", lookup.calls)
				}
				assertStageApprovalLookupNoMutation(t, h)
			})
		}
	}
}
