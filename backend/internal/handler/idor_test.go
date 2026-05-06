package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cooker-ci/cooker/internal/model"
)

// TestIDOR_RunIdMustMatchPipelineId enforces the T3 fix: every
// runId-shaped endpoint refuses to operate when :runId belongs to
// a different pipeline than :id.
func TestIDOR_RunIdMustMatchPipelineId(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	if err := h.Store.Pipelines.Create(ctx, &model.Pipeline{ID: "victim", Name: "victim"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Pipelines.Create(ctx, &model.Pipeline{ID: "attacker", Name: "attacker"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Runs.Create(ctx, &model.PipelineRun{ID: "secret-run", PipelineID: "victim"}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		method string
		path   string
		route  string
		setup  func(r *gin.Engine)
	}{
		{
			"GetPipelineRun",
			http.MethodGet,
			"/pipelines/attacker/runs/secret-run",
			"/pipelines/:id/runs/:runId",
			func(r *gin.Engine) { r.GET("/pipelines/:id/runs/:runId", h.GetPipelineRun) },
		},
		{
			"CancelPipelineRun",
			http.MethodPost,
			"/pipelines/attacker/runs/secret-run/cancel",
			"/pipelines/:id/runs/:runId/cancel",
			func(r *gin.Engine) { r.POST("/pipelines/:id/runs/:runId/cancel", h.CancelPipelineRun) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			tc.setup(r)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("expected 404 (cross-pipeline access denied), got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
