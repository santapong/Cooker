package handler

import (
	"context"

	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/triage"
)

type fakeTriage struct {
	res triage.Result
	err error
	got triage.Request
}

func (f *fakeTriage) Triage(_ context.Context, req triage.Request) (triage.Result, error) {
	f.got = req
	return f.res, f.err
}

func triageRouter(h *Handler) *gin.Engine {
	op := &auth.Claims{Email: "op@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/stages/:stageId/triage", withUser(h.TriageStage, op))
	r.GET("/capabilities", withUser(h.GetCapabilities, op))
	return r
}

func seedFailedRun(t *testing.T, h *Handler) {
	t.Helper()
	ctx := context.Background()
	if err := h.Store.Pipelines.Create(ctx, &model.Pipeline{
		ID: "p1", Name: "P",
		Stages: []model.Stage{{ID: "build", Name: "Build", Type: model.StageTypeBuild}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Runs.Create(ctx, &model.PipelineRun{
		ID: "r1", PipelineID: "p1", Status: model.RunStatusFailed,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusFailed, Error: "exit 1", Logs: "compile error"},
			{StageID: "push", Status: model.RunStatusSuccess},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTriageStage_Disabled503AndCapabilityOff(t *testing.T) {
	h := newTestHandler(t)
	seedFailedRun(t, h)
	r := triageRouter(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/pipelines/p1/runs/r1/stages/build/triage", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
	if !strings.Contains(w.Body.String(), `"aiTriage":false`) {
		t.Errorf("capabilities = %s", w.Body.String())
	}
}

func TestTriageStage_NonFailedStage409(t *testing.T) {
	h := newTestHandler(t)
	h.Triage = &fakeTriage{}
	seedFailedRun(t, h)
	w := httptest.NewRecorder()
	triageRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/pipelines/p1/runs/r1/stages/push/triage", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409: %s", w.Code, w.Body.String())
	}
}

func TestTriageStage_Happy(t *testing.T) {
	h := newTestHandler(t)
	ft := &fakeTriage{res: triage.Result{Advisory: "the compiler is sad", Model: "claude-fable-5"}}
	h.Triage = ft
	seedFailedRun(t, h)

	w := httptest.NewRecorder()
	triageRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/pipelines/p1/runs/r1/stages/build/triage", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "the compiler is sad") {
		t.Errorf("body = %s", w.Body.String())
	}
	if !strings.Contains(ft.got.Prompt, "compile error") || !strings.Contains(ft.got.Prompt, "Build") {
		t.Errorf("prompt missing context: %q", ft.got.Prompt)
	}
}

func TestTriageStage_RateLimited429(t *testing.T) {
	h := newTestHandler(t)
	h.Triage = &fakeTriage{err: &triage.APIError{Status: http.StatusTooManyRequests, Type: "rate_limit_error"}}
	seedFailedRun(t, h)
	w := httptest.NewRecorder()
	triageRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/pipelines/p1/runs/r1/stages/build/triage", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("code = %d, want 429: %s", w.Code, w.Body.String())
	}
}
