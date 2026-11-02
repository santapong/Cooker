package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

type runtimeLogFailingApps struct {
	store.AppStore
	err error
}

func (s runtimeLogFailingApps) Get(context.Context, string) (*model.App, error) {
	return nil, s.err
}

func TestRuntimeLogSnapshotAppReadFailureStaysUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{{"missing", store.ErrNotFound, 404}, {"nil app", nil, 404}, {"cancelled", context.Canceled, 503}, {"storage failure", errors.New("private-storage-canary"), 503}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(t)
			h.Runtime = &service.RuntimeService{Store: h.Store}
			h.Store.Apps = runtimeLogFailingApps{AppStore: h.Store.Apps, err: tc.err}
			r := gin.New()
			r.GET("/apps/:id/services/:svc/log-replicas", h.GetServiceLogReplicas)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/apps/owned/services/web/log-replicas", nil))
			if w.Code != tc.want || strings.Contains(w.Body.String(), "private-storage-canary") || (tc.want == 503 && strings.Contains(w.Body.String(), "app not found")) {
				t.Fatal("storage failure classification or diagnostic boundary changed")
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("failed log response cache policy absent")
			}
		})
	}
}

func TestRuntimeLogSnapshotHTTPRejectsInvalidBodyBeforeCapture(t *testing.T) {
	h := newTestHandler(t)
	if err := h.Store.Apps.Create(context.Background(), &model.App{ID: "owned", Name: "Owned"}); err != nil {
		t.Fatal(err)
	}
	h.Runtime = &service.RuntimeService{Store: h.Store, DockerBin: "nonexistent-owned-log-cli"}
	r := gin.New()
	r.POST("/apps/:id/services/:svc/log-sessions", h.OpenServiceLogSession)
	for _, body := range []string{`{"unknown":"private-input"}`, `{} {}`, `{"maxLines":"bad"}`, `{"search":"` + strings.Repeat("x", 17000) + `"}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/apps/owned/services/web/log-sessions", strings.NewReader(body))
		r.ServeHTTP(w, req)
		if w.Code != 400 || strings.Contains(w.Body.String(), "private-input") || strings.Contains(w.Body.String(), "nonexistent-owned") {
			t.Fatal("invalid input reached capture or reflected private diagnostic")
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("log response cache policy absent")
		}
	}
}

func TestRuntimeLogSnapshotHTTPPagingDownloadAndClose(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	app := &model.App{ID: "owned", Name: "Owned"}
	if err := h.Store.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	pipeline := &model.Pipeline{ID: "pipeline", Stages: []model.Stage{{ID: "web", Type: model.StageTypeDeploy, Config: model.StageConfig{RuntimeName: "owned-app", ComposeServiceName: "web", DeployRuntime: "docker"}}}}
	if err := h.Store.Pipelines.Create(ctx, pipeline); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.AppDeploys.Create(ctx, &model.AppDeploy{ID: "deployment", AppID: app.ID, RunID: "run", PipelineID: pipeline.ID, Status: model.RunStatusSuccess}); err != nil {
		t.Fatal(err)
	}
	h.Jobs = jobqueue.NewMemory()
	payload, _ := json.Marshal(service.AppDeployPlan{Version: 1, RunID: "run", App: app})
	if _, err := h.Jobs.Enqueue(ctx, service.JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:run"}); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "docker")
	script := "#!/bin/sh\ncase \"$1\" in\ninspect) printf '%s\\n' '" + strings.Repeat("a", 64) + "|run|/owned-app|<no value>|<no value>|<no value>|running';;\nlogs) printf '%s\\n' '2026-10-01T00:00:00Z first' '2026-10-01T00:00:01Z second';;\n*) exit 1;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	h.Runtime = &service.RuntimeService{Store: h.Store, DockerBin: bin, SecretGenerationBound: true}
	r := gin.New()
	base := "/apps/:id/services/:svc/log-sessions"
	r.POST(base, h.OpenServiceLogSession)
	r.GET(base+"/:sessionId", h.GetServiceLogSession)
	r.GET(base+"/:sessionId/download", h.DownloadServiceLogSession)
	r.DELETE(base+"/:sessionId", h.CloseServiceLogSession)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	w := send("POST", "/apps/owned/services/web/log-sessions", `{}`)
	if w.Code != 201 {
		t.Fatal("snapshot open failed", w.Code)
	}
	var page service.RuntimeLogPage
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Total != 2 || page.SessionID == "" {
		t.Fatal("snapshot response malformed")
	}
	path := "/apps/owned/services/web/log-sessions/" + page.SessionID
	w = send("GET", path+"?offset=1&limit=1", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"sequence":2`) {
		t.Fatal("HTTP cursor did not select stable entry")
	}
	w = send("GET", path+"/download", "")
	if w.Code != 200 || w.Header().Get("Content-Disposition") != `attachment; filename="cooker-service-logs.txt"` || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "second") {
		t.Fatal("redacted download response unavailable")
	}
	if w := send("DELETE", path, ""); w.Code != 204 {
		t.Fatal("session close failed")
	}
	if w := send("GET", path, ""); w.Code != 404 {
		t.Fatal("closed session remained readable")
	}
	r.GET("/private-error", func(c *gin.Context) { runtimeLogHTTPError(c, errors.New("private diagnostic")) })
	if w := send("GET", "/private-error", ""); w.Code != 503 || strings.Contains(w.Body.String(), "private diagnostic") {
		t.Fatal("unexpected service error leaked")
	}
}
