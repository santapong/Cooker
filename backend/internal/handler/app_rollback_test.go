package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

func rollbackRouter(h *Handler) *gin.Engine {
	op := &auth.Claims{Email: "op@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/apps/:id/rollback", withUser(h.RollbackApp, op))
	r.GET("/apps/:id/deploys", withUser(h.ListAppDeploys, op))
	r.GET("/apps/:id/drift", withUser(h.GetAppDrift, op))
	return r
}

func seedRollbackApp(t *testing.T, h *Handler, kind model.DeployTargetKind) *model.App {
	t.Helper()
	a := &model.App{
		ID: "app-1", Name: "shop", GitHubRepo: "acme/shop",
		DeployTarget: model.DeployTarget{Kind: kind, Namespace: "prod"},
	}
	if err := h.Store.Apps.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func seedHistory(t *testing.T, h *Handler, n int) {
	t.Helper()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		if err := h.Store.AppDeploys.Create(context.Background(), &model.AppDeploy{
			ID: "dep-" + string(rune('a'+i)), AppID: "app-1", RunID: "run",
			ImageRef: "reg/shop:" + string(rune('0'+i)), Status: model.RunStatusSuccess,
			Kind: model.AppDeployKindDeploy, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRollbackApp_NoHistory404(t *testing.T) {
	h := newTestHandler(t)
	h.AppDeployer = service.NewAppDeployer(service.NewExecutor(), "reg")
	h.AppDeployer.Deploys = h.Store.AppDeploys
	seedRollbackApp(t, h, model.DeployTargetKubernetes)

	w := httptest.NewRecorder()
	rollbackRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apps/app-1/rollback", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestRollbackApp_OneDeployOnly404(t *testing.T) {
	h := newTestHandler(t)
	h.AppDeployer = service.NewAppDeployer(service.NewExecutor(), "reg")
	h.AppDeployer.Deploys = h.Store.AppDeploys
	seedRollbackApp(t, h, model.DeployTargetKubernetes)
	seedHistory(t, h, 1)

	w := httptest.NewRecorder()
	rollbackRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apps/app-1/rollback", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404 (no earlier deploy): %s", w.Code, w.Body.String())
	}
}

func TestRollbackApp_NonKubernetes409(t *testing.T) {
	h := newTestHandler(t)
	h.AppDeployer = service.NewAppDeployer(service.NewExecutor(), "reg")
	h.AppDeployer.Deploys = h.Store.AppDeploys
	seedRollbackApp(t, h, model.DeployTargetDockerHost)
	seedHistory(t, h, 2)

	w := httptest.NewRecorder()
	rollbackRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apps/app-1/rollback", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409: %s", w.Code, w.Body.String())
	}
}

func TestRollbackApp_Happy202RollsToPrevious(t *testing.T) {
	h := newTestHandler(t)
	h.AppDeployer = service.NewAppDeployer(service.NewExecutor(), "reg")
	h.AppDeployer.Deploys = h.Store.AppDeploys
	seedRollbackApp(t, h, model.DeployTargetKubernetes)
	seedHistory(t, h, 3) // newest reg/shop:2; previous reg/shop:1

	w := httptest.NewRecorder()
	rollbackRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apps/app-1/rollback", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !containsStr(body, `"imageRef":"reg/shop:1"`) {
		t.Errorf("expected rollback target reg/shop:1, got %s", body)
	}

	// The legacy goroutine path executes the rollback; wait for the
	// history row it records (noop executor → success, fast).
	deadline := time.Now().Add(3 * time.Second)
	for {
		rows, err := h.Store.AppDeploys.ListByApp(context.Background(), "app-1", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) >= 4 && rows[0].Kind == model.AppDeployKindRollback {
			if rows[0].ImageRef != "reg/shop:1" {
				t.Errorf("rollback row image = %s, want reg/shop:1", rows[0].ImageRef)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rollback history row never appeared; have %d rows", len(rows))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestListAppDeploys_EmptyOK(t *testing.T) {
	h := newTestHandler(t)
	seedRollbackApp(t, h, model.DeployTargetKubernetes)
	w := httptest.NewRecorder()
	rollbackRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apps/app-1/deploys", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), `"deploys":[]`) {
		t.Errorf("expected empty deploys array, got %s", w.Body.String())
	}
}

func TestGetAppDrift_UnsupportedTarget(t *testing.T) {
	h := newTestHandler(t)
	seedRollbackApp(t, h, model.DeployTargetDockerHost)
	w := httptest.NewRecorder()
	rollbackRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apps/app-1/drift", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), `"status":"unsupported"`) {
		t.Errorf("expected unsupported, got %s", w.Body.String())
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
