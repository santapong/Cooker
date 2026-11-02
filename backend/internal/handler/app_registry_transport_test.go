package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

func TestAppRegistryUnsupportedTransportHTTPRejectsBeforeRun(t *testing.T) {
	h := newTestHandler(t)
	app := &model.App{ID: "owned-kube", Name: "owned", GitHubRepo: "owned/fixture", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes}}
	if err := h.Store.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	h.AppDeployer = service.NewAppDeployer(service.NewExecutor(service.WithRegistryAuthority(service.NewRegistryAuthorityService(h.Store, nil))), "ghcr.io/owned")
	spawner := &captureSpawner{}
	h.Runs = spawner
	admin := &auth.Claims{Subject: "owned-admin", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/apps/:id/deploy", withUser(h.DeployApp, admin))
	r.POST("/apps/:id/rollback", withUser(h.RollbackApp, admin))
	for _, action := range []string{"deploy", "rollback"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPost, "/apps/"+app.ID+"/"+action, map[string]string{}))
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("unsupported %s accepted: %d %s", action, w.Code, w.Body.String())
		}
	}
	runs, err := h.Store.Runs.List(context.Background(), "", 0, 0)
	if err != nil || len(runs) != 0 || len(spawner.runIDs) != 0 {
		t.Fatal("unsupported transport created/spawned a run")
	}
}

func TestAppCapabilitiesRespectScopedRegistryTransport(t *testing.T) {
	h := newTestHandler(t)
	h.AppDeployer = service.NewAppDeployer(service.NewExecutor(service.WithRegistryAuthority(service.NewRegistryAuthorityService(h.Store, nil))), "ghcr.io/owned")
	checks := map[model.DeployTargetKind][]bool{}
	h.AppDeployer.CheckExecution = func(app *model.App, build bool) error {
		checks[app.DeployTarget.Kind] = append(checks[app.DeployTarget.Kind], build)
		return nil
	}
	h.Runtime = &service.RuntimeService{Store: h.Store}
	h.Jobs = jobqueue.NewMemory()
	r := gin.New()
	r.GET("/capabilities", h.AppDeploymentCapabilities)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("capabilities status %d", w.Code)
	}
	var body struct {
		Targets []service.TargetCapability `json:"targets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Targets) != 4 {
		t.Fatalf("missing target capabilities: %+v", body.Targets)
	}
	for _, cap := range body.Targets {
		if cap.Kind == model.DeployTargetDockerHost {
			if !cap.Available || !cap.SourceBuild.Available || !cap.ServiceLogs.Available || len(checks[cap.Kind]) != 2 {
				t.Fatalf("supported Docker capabilities hidden: %+v, checks %v", cap, checks[cap.Kind])
			}
			continue
		}
		if cap.Available || cap.SourceBuild.Available || cap.ServiceLogs.Available || !strings.Contains(cap.Reason, "scoped registry deployment") {
			t.Fatalf("unsupported scoped transport advertised: %+v", cap)
		}
		if len(checks[cap.Kind]) != 0 {
			t.Fatalf("unavailable target ran execution checks: %s %v", cap.Kind, checks[cap.Kind])
		}
	}
}
