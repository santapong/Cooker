package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/catalog"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/provider/vercel"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestCatalogRegisteredRoutesAuthenticateAndCannotExecuteImports(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	st := memory.New()
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := local.NewIssuer(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	viewer, _, err := issuer.Issue("viewer", "viewer@fixture.invalid", "Viewer", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	admin, _, err := issuer.Issue("admin", "admin@fixture.invalid", "Admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	mw, err := auth.NewMiddleware(ctx, config.OIDCConfig{})
	if err != nil {
		t.Fatal(err)
	}
	mw.EnableLocalAuth(issuer)
	vstore := vercel.NewMemory()
	now := time.Now().UTC()
	err = vstore.Create(ctx, &vercel.Record{Connection: vercel.Connection{ID: "connection", Identity: vercel.Identity{ID: "user_fixture", Scope: "personal"}, ProjectIDs: []string{"prj_fixture"}, Enabled: true, LastSuccessAt: &now}, Snapshot: vercel.Snapshot{Projects: []vercel.Project{{ID: "prj_fixture", Name: "Synced"}}, Deployments: []vercel.Deployment{}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Apps.Create(ctx, &model.App{ID: "native", Name: "Native", WebhookSecret: []byte("private-webhook-canary"), DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}); err != nil {
		t.Fatal(err)
	}
	h := handler.New(st, codec, nil)
	h.Vercel = &vercel.Service{Store: vstore, Codec: codec, API: vercelFixture{}}
	r := gin.New()
	s := &Server{router: r, handler: h, config: &config.Config{}, oidcMW: mw}
	s.registerRoutes()
	send := func(token, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), "canary") {
			t.Fatal("secret escaped catalog")
		}
		return w
	}
	for _, path := range []string{"/catalog/apps", "/catalog/apps/missing", "/catalog/services", "/catalog/pipelines", "/catalog/pipelines/missing"} {
		if w := send("", "GET", "/api/v1"+path, ""); w.Code != 401 {
			t.Fatal("unauthenticated catalog admitted", path, w.Code)
		}
	}
	w := send(viewer, "GET", "/api/v1/catalog/apps", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var apps catalog.List[catalog.App]
	if err = json.Unmarshal(w.Body.Bytes(), &apps); err != nil {
		t.Fatal(err)
	}
	if apps.Total != 2 {
		t.Fatal("catalog count", apps)
	}
	var appID, pipelineID string
	for _, a := range apps.Items {
		if a.Capabilities.Edit || a.Capabilities.Deploy {
			t.Fatal("viewer has write capability")
		}
		if a.Management == "vercel" {
			appID = a.ID
			pipelineID = a.PipelineID
		}
	}
	for _, path := range []string{"/catalog/apps/" + appID, "/catalog/services?environment=production_first", "/catalog/pipelines", "/catalog/pipelines/" + pipelineID} {
		if w := send(viewer, "GET", "/api/v1"+path, ""); w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	for _, request := range []struct{ method, path, body string }{{"PUT", "/api/v1/apps/" + appID, `{"name":"overwrite"}`}, {"DELETE", "/api/v1/apps/" + appID, ""}, {"PUT", "/api/v1/pipelines/" + pipelineID, `{"name":"overwrite","stages":[],"edges":[]}`}, {"DELETE", "/api/v1/pipelines/" + pipelineID, ""}, {"POST", "/api/v1/catalog/apps", `{}`}, {"POST", "/api/v1/catalog/pipelines/" + pipelineID, `{}`}} {
		if w := send(admin, request.method, request.path, request.body); w.Code < 400 {
			t.Fatal("import gained write path", request, w.Code)
		}
	}
	for _, path := range []string{"/api/v1/apps/" + appID + "/deploy", "/api/v1/pipelines/" + pipelineID + "/run"} {
		if w := send(admin, "POST", path, `{}`); w.Code != 404 {
			t.Fatal("imported identity reached execution admission", path, w.Code, w.Body.String())
		}
	}
	runs, _ := st.Runs.List(ctx, "", 0, 0)
	if len(runs) != 0 {
		t.Fatal("import execution request created a run")
	}
	nativeApps, _ := st.Apps.List(ctx, 0, 0)
	nativePipelines, _ := st.Pipelines.List(ctx, 0, 0)
	if len(nativeApps) != 1 || len(nativePipelines) != 0 {
		t.Fatal("catalog wrote executable records")
	}
	if w := send(admin, "GET", "/api/v1/catalog/apps/native", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"edit":true`) {
		t.Fatal("native writer capabilities lost", w.Code, w.Body.String())
	}
}
