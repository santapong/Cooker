package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

func TestAppPrefixCannotBeBypassedUsingClientID(t *testing.T) {
	h := newTestHandler(t)
	existing := &model.App{ID: "already-exists", Name: "shop", DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes, Prefix: "shop"}}
	if err := h.Store.Apps.Create(context.Background(), existing); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/apps", h.CreateApp)
	body := []byte(`{"id":"already-exists","name":"another","githubRepo":"acme/shop","branch":"main","deployTarget":{"kind":"kubernetes","prefix":"shop","namespace":"default"}}`)
	req := httptest.NewRequest("POST", "/apps", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("prefix bypass accepted: %d %s", w.Code, w.Body)
	}
}

func TestAppDeploymentViewIsAvailableDuringCheckoutAndFailure(t *testing.T) {
	h := newTestHandler(t)
	h.Runs = &captureSpawner{}
	h.AppDeployer = &service.AppDeployer{}
	app := &model.App{ID: "shop", Name: "shop", GitHubRepo: "acme/shop", Branch: "main", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes, Prefix: "shop"}}
	if err := h.Store.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/apps/:id/deploy", h.DeployApp)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/apps/shop/deploy", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var res struct {
		PipelineID string `json:"pipelineId"`
		RunID      string `json:"runId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	p, err := h.Store.Pipelines.Get(context.Background(), res.PipelineID)
	if err != nil || p == nil {
		t.Fatal("deployment view unavailable during clone")
	}
	run, err := h.Store.Runs.Get(context.Background(), res.RunID)
	if err != nil || run.PipelineID != res.PipelineID || run.Status != model.RunStatusRunning {
		t.Fatalf("stub is not addressable: %+v %v", run, err)
	}
	// A preflight error cannot produce stages; its run must still finish with
	// the same addressable pipeline ID and an explanation.
	app.DeployTarget.Kind = "unsupported"
	h.runAppDeployCtx(context.Background(), app, res.RunID, "test")
	run, err = h.Store.Runs.Get(context.Background(), res.RunID)
	if err != nil || run.Status != model.RunStatusFailed || run.Error == "" || run.PipelineID != res.PipelineID {
		t.Fatalf("failed checkout left an unusable run: %+v %v", run, err)
	}
}

func TestAppCapabilitiesDistinguishRuntimeFromSourceBuild(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.GET("/capabilities", h.AppDeploymentCapabilities)
	read := func() []service.TargetCapability {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/capabilities", nil))
		var body struct {
			Targets []service.TargetCapability `json:"targets"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Targets
	}
	for _, c := range read() {
		if c.Available || c.SourceBuild.Available {
			t.Fatal("nil deployer advertised execution")
		}
	}
	h.AppDeployer = &service.AppDeployer{CheckExecution: func(_ *model.App, build bool) error {
		if build {
			return fmt.Errorf("no compatible builder")
		}
		return nil
	}}
	for _, c := range read() {
		if c.Kind == model.DeployTargetDockerHost {
			if !c.Available || c.SourceBuild.Available || c.SourceBuild.Reason != "no compatible builder" || c.ServiceLogs.Available {
				t.Fatalf("misleading capabilities: %+v", c)
			}
		}
	}
	h.Runtime = &service.RuntimeService{Store: h.Store}
	h.Jobs = jobqueue.NewMemory()
	for _, c := range read() {
		if c.Kind == model.DeployTargetKubernetes && (c.ServiceLogs.Available || !strings.Contains(c.ServiceLogs.Reason, "immutable pod session")) {
			t.Fatalf("unavailable Kubernetes log sessions advertised: %+v", c)
		}
		if c.Kind == model.DeployTargetDockerHost && !c.ServiceLogs.Available {
			t.Fatalf("configured Docker log sessions missing: %+v", c)
		}
	}
}
