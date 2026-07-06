package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

// canaryWeightedStub is a no-op WeightedDeployer for handler tests: it
// satisfies the canary capability so Start/Promote/Abort run without a
// real cluster.
type canaryWeightedStub struct{}

func (canaryWeightedStub) Deploy(context.Context, deployer.Request) (deployer.Result, error) {
	return deployer.Result{}, nil
}
func (canaryWeightedStub) DeployWeighted(context.Context, deployer.WeightedRequest) (deployer.WeightedResult, error) {
	return deployer.WeightedResult{CanaryReplicas: 1, StableReplicas: 3}, nil
}

// canaryBuilderStub stands in for AppDeployer.BuildAndPushImage.
type canaryBuilderStub struct{}

func (canaryBuilderStub) BuildAndPushImage(context.Context, *model.App, string, io.Writer) (string, *model.Pipeline, *model.PipelineRun, error) {
	return "reg/shop:new", &model.Pipeline{ID: "p"}, &model.PipelineRun{ID: "r", Status: model.RunStatusSuccess}, nil
}

func canaryRouter(h *Handler) *gin.Engine {
	op := &auth.Claims{Email: "op@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.GET("/apps/:id", withUser(h.GetApp, op))
	r.GET("/apps/:id/canary", withUser(h.GetAppCanary, op))
	r.POST("/apps/:id/canary/promote", withUser(h.PromoteAppCanary, op))
	r.POST("/apps/:id/canary/abort", withUser(h.AbortAppCanary, op))
	return r
}

// wireCanary attaches a CanaryService to h. weighted nil => unsupported.
func wireCanary(h *Handler, weighted deployer.WeightedDeployer) {
	h.Canary = service.NewCanaryService(h.Store.Apps, h.Store.AppCanaries, h.Store.AppDeploys, canaryBuilderStub{}, weighted)
}

func seedCanaryApp(t *testing.T, h *Handler) *model.App {
	t.Helper()
	a := &model.App{
		ID: "app-1", Name: "shop", GitHubRepo: "acme/shop", Branch: "main",
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes, Namespace: "prod"},
		Canary:       model.CanaryConfig{Strategy: model.DeployStrategyCanary, Weight: 20, AutoPromote: false},
	}
	if err := h.Store.Apps.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestGetAppCanary_NoneActive404(t *testing.T) {
	h := newTestHandler(t)
	wireCanary(h, canaryWeightedStub{})
	seedCanaryApp(t, h)
	w := httptest.NewRecorder()
	canaryRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apps/app-1/canary", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestCanaryPromote_NoneActive404(t *testing.T) {
	h := newTestHandler(t)
	wireCanary(h, canaryWeightedStub{})
	seedCanaryApp(t, h)
	w := httptest.NewRecorder()
	canaryRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apps/app-1/canary/promote", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestCanaryPromoteAndAbort_HappyPaths(t *testing.T) {
	h := newTestHandler(t)
	wireCanary(h, canaryWeightedStub{})
	app := seedCanaryApp(t, h)

	// Start a canary directly through the service so we have one in flight.
	if _, err := h.Canary.Start(context.Background(), app, "run-1", io.Discard); err != nil {
		t.Fatalf("seed canary: %v", err)
	}

	// GET /apps/:id embeds the live canary state under "activeCanary".
	w := httptest.NewRecorder()
	canaryRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apps/app-1", nil))
	if w.Code != http.StatusOK || !containsStr(w.Body.String(), `"activeCanary"`) {
		t.Fatalf("GET app should embed activeCanary: code=%d body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), `"status":"progressing"`) {
		t.Errorf("expected progressing canary in app body, got %s", w.Body.String())
	}

	// Promote.
	w = httptest.NewRecorder()
	canaryRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apps/app-1/canary/promote", nil))
	if w.Code != http.StatusOK || !containsStr(w.Body.String(), `"status":"promoted"`) {
		t.Fatalf("promote: code=%d body=%s", w.Code, w.Body.String())
	}

	// After promote there is no active canary, so a second promote 404s.
	w = httptest.NewRecorder()
	canaryRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apps/app-1/canary/promote", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("second promote should 404, got %d", w.Code)
	}
}

func TestCanaryAbort_HappyPath(t *testing.T) {
	h := newTestHandler(t)
	wireCanary(h, canaryWeightedStub{})
	app := seedCanaryApp(t, h)
	if _, err := h.Canary.Start(context.Background(), app, "run-1", io.Discard); err != nil {
		t.Fatalf("seed canary: %v", err)
	}
	w := httptest.NewRecorder()
	canaryRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apps/app-1/canary/abort", nil))
	if w.Code != http.StatusOK || !containsStr(w.Body.String(), `"status":"aborted"`) {
		t.Fatalf("abort: code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCanaryService_Unwired503(t *testing.T) {
	h := newTestHandler(t) // h.Canary stays nil
	seedCanaryApp(t, h)
	w := httptest.NewRecorder()
	canaryRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apps/app-1/canary/promote", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil canary service should 503, got %d", w.Code)
	}
}

func TestValidateAppInput_RejectsBadCanaryWeight(t *testing.T) {
	err := validateAppInput(&model.App{
		Name: "shop", GitHubRepo: "acme/shop", Branch: "main",
		Canary: model.CanaryConfig{Strategy: model.DeployStrategyCanary, Weight: 150},
	})
	if err == nil {
		t.Fatal("expected validation error for canary weight 150")
	}
}

func TestValidateAppInput_NormalizesCanary(t *testing.T) {
	a := &model.App{
		Name: "shop", GitHubRepo: "acme/shop", Branch: "main",
		Canary: model.CanaryConfig{Strategy: model.DeployStrategyCanary}, // no weight
	}
	if err := validateAppInput(a); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Canary.Weight != model.DefaultCanaryWeight {
		t.Errorf("canary weight not defaulted: got %d", a.Canary.Weight)
	}
}
