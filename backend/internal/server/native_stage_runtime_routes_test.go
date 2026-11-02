package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/provider/vercel"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store/memory"
)

const nativeStatusRoute = "/api/v1/pipelines/:id/runs/:runId/stages/:stageId/runtime"

type nativeStatusRouteFixture struct {
	s          *Server
	issuer     *local.Issuer
	jobs       *jobqueue.Memory
	p          *model.Pipeline
	run        *model.PipelineRun
	dir, calls string
}

func newNativeStatusRouteFixture(t *testing.T, withRuntime bool) *nativeStatusRouteFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	issuer, err := local.NewIssuer(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mw, err := auth.NewMiddleware(context.Background(), config.OIDCConfig{})
	if err != nil {
		t.Fatal(err)
	}
	mw.EnableLocalAuth(issuer)
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	h := handler.New(memory.New(), codec, nil)
	f := &nativeStatusRouteFixture{issuer: issuer, jobs: jobqueue.NewMemory(), dir: t.TempDir()}
	f.s = &Server{router: gin.New(), handler: h, config: &config.Config{}, oidcMW: mw}
	h.Jobs = f.jobs
	if withRuntime {
		f.addAcceptedRuntime(t)
	}
	f.s.registerRoutes()
	t.Cleanup(func() {
		if err := f.s.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

// EnqueuePlannedRun is the actual producer. Queue transitions and the recorded
// successful run are unit fixtures only; no worker or container is deployed.
func (f *nativeStatusRouteFixture) addAcceptedRuntime(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	h := f.s.handler
	f.p = &model.Pipeline{ID: "route-native", Name: "Native", Version: 1, UpdatedAt: time.Now().UTC(), Stages: []model.Stage{{ID: "deploy-web", Name: "Web", Type: model.StageTypeDeploy,
		Config: model.StageConfig{DeployRuntime: "docker", RuntimeName: "web", Image: "registry.invalid/web:v1"}}}}
	f.p.SetDefaults()
	if err := h.Store.Pipelines.Create(ctx, f.p); err != nil {
		t.Fatal(err)
	}
	planned := &model.PipelineRun{ID: "route-run", PipelineID: f.p.ID, PipelineVersion: f.p.Version, Status: model.RunStatusPending, CreatedAt: time.Now().UTC(),
		Variables: map[string]string{}, StartedByUserSub: "accepted-other-author", StartedByEmail: "author@fixture.invalid",
		StartedByTokenHash: "private-route-actor-canary", StageRuns: []model.StageRun{{StageID: "deploy-web", Status: model.RunStatusPending}}}
	if err := service.NewJobQueueEnqueuer(f.jobs, h.Store.Pipelines).EnqueuePlannedRun(ctx, f.p, planned); err != nil {
		t.Fatal(err)
	}
	j, err := f.jobs.Dequeue(ctx, "unit-fixture", []string{service.JobKindPipelineRun})
	if err != nil || j == nil || j.ID != "run:"+planned.ID {
		t.Fatal("accepted route queue snapshot unavailable", err)
	}
	if err = f.jobs.Complete(ctx, "unit-fixture", j.ID); err != nil {
		t.Fatal(err)
	}
	f.run = planned.Clone()
	start, finish := time.Now().Add(-2*time.Second).UTC(), time.Now().UTC()
	f.run.Status, f.run.StartedAt, f.run.FinishedAt = model.RunStatusSuccess, &start, &finish
	sr := &f.run.StageRuns[0]
	sr.Status, sr.StartedAt, sr.FinishedAt = model.RunStatusSuccess, &start, &finish
	sr.Logs = "private-route-logs-canary"
	sr.RuntimeReceipt = &model.RuntimeReceipt{Version: 1, Runtime: "docker", RunID: f.run.ID, StageID: sr.StageID, ContainerID: strings.Repeat("a", 64), Name: "web",
		ImageReference: "registry.invalid/web:v1", ImageID: "sha256:" + strings.Repeat("b", 64), CapturedAt: start.Add(time.Second)}
	if err = h.Store.Runs.Create(ctx, f.run); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(f.dir, "docker-native-route")
	f.calls = filepath.Join(f.dir, "calls")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$COOKER_NATIVE_ROUTE_DIR/calls"
[ "$1" = container ] && [ "$2" = inspect ] && [ "$3" = --format ] || exit 91
cat "$COOKER_NATIVE_ROUTE_DIR/response.json"
`
	if err = os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal([]string{sr.RuntimeReceipt.ContainerID, "/web", sr.RuntimeReceipt.ImageID, sr.RuntimeReceipt.ImageReference, f.run.ID, sr.StageID, "running", "absent", ""})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.dir, "response.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COOKER_NATIVE_ROUTE_DIR", f.dir)
	h.Runtime = &service.RuntimeService{Store: h.Store, DockerBin: cli}
}

func (f *nativeStatusRouteFixture) send(t *testing.T, role, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if role != "" {
		token, _, err := f.issuer.Issue(role, role+"@fixture.invalid", role, role)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.s.router.ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), "canary") {
		t.Fatal("private retained inputs escaped authenticated native status")
	}
	return w
}

func nativeRouteHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("registered status response missing headers")
	}
}

func (f *nativeStatusRouteFixture) path() string {
	return "/api/v1/pipelines/" + f.p.ID + "/runs/" + f.run.ID + "/stages/deploy-web/runtime"
}

func TestNativeStageRuntimeRegisteredRouteAuthenticationAndReadRoles(t *testing.T) {
	f := newNativeStatusRouteFixture(t, false)
	path := "/api/v1/pipelines/native/runs/run/stages/deploy/runtime"
	for _, role := range []string{"", "viewer", "approver", "operator", "admin"} {
		t.Run(role, func(t *testing.T) {
			w := f.send(t, role, path)
			want := 503
			if role == "" {
				want = 401
			}
			if w.Code != want {
				t.Fatal("registered read route role contract", role, w.Code, w.Body.String())
			}
			if role != "" {
				nativeRouteHeaders(t, w)
				var body map[string]string
				if json.Unmarshal(w.Body.Bytes(), &body) != nil || !reflect.DeepEqual(body, map[string]string{"error": service.ErrNativeRuntimeUnavailable.Error()}) {
					t.Fatal("dependency error exposed nonfixed response")
				}
			}
		})
	}
	count := 0
	for _, route := range f.s.router.Routes() {
		if route.Path == nativeStatusRoute {
			count++
			if route.Method != http.MethodGet || !strings.HasSuffix(route.Handler, "GetNativeStageRuntime-fm") {
				t.Fatal("native status acquired an execution or unexpected handler route", route)
			}
		}
	}
	if count != 1 {
		t.Fatal("native status must expose exactly one registered GET", count)
	}
}

func (f *nativeStatusRouteFixture) snapshot(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	st := f.s.handler.Store
	p, err := st.Pipelines.List(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	r, err := st.Runs.List(ctx, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	full, err := st.Runs.Get(ctx, f.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	j, err := f.jobs.Get(ctx, "run:"+f.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := f.jobs.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	apps, err := st.Apps.List(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	history, err := st.AppDeploys.ListByApp(ctx, "not-an-app", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal([]any{p, r, full, j, stats, apps, history})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNativeStageRuntimeRegisteredExactObservationIsReadOnly(t *testing.T) {
	f := newNativeStatusRouteFixture(t, true)
	before := f.snapshot(t)
	for _, role := range []string{"viewer", "approver", "operator", "admin"} {
		w := f.send(t, role, f.path()+"?containerId=foreign&stageId=foreign&runId=foreign")
		nativeRouteHeaders(t, w)
		var s service.NativeRuntimeStatus
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil || s.Binding != "verified" || s.Subject != (service.NativeRuntimeSubject{Kind: "native-stage", PipelineID: f.p.ID, RunID: f.run.ID, StageID: "deploy-web"}) || s.ContainerID != strings.Repeat("a", 64) || s.RecordedStageStatus != model.RunStatusSuccess || s.State != "running" || s.Health != "not_configured" || !reflect.DeepEqual(s.SupportedActions, []string{"inspect"}) {
			t.Fatal("registered role did not receive exact typed status", role, w.Code, w.Body.String())
		}
	}
	data, err := os.ReadFile(f.calls)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 20 {
		t.Fatal("unexpected inspect count")
	}
	for i := 0; i < len(lines); i += 5 {
		if lines[i] != "container" || lines[i+1] != "inspect" || lines[i+2] != "--format" || lines[i+4] != strings.Repeat("a", 64) || strings.Contains(lines[i+3], ".Config.Env") || strings.Contains(lines[i+3], ".State.Health.Log") {
			t.Fatal("registered status used unsafe or foreign inspect arguments")
		}
	}
	if f.snapshot(t) != before {
		t.Fatal("registered native read mutated accepted or application records")
	}
}

type nativeStatusProviderCounter struct{ calls int }

func (p *nativeStatusProviderCounter) Verify(context.Context, string, string) (vercel.Identity, error) {
	p.calls++
	return vercel.Identity{}, errorsForNativeProvider()
}
func (p *nativeStatusProviderCounter) Projects(context.Context, string, string) ([]vercel.Project, error) {
	p.calls++
	return nil, errorsForNativeProvider()
}
func (p *nativeStatusProviderCounter) Deployments(context.Context, string, string, string) ([]vercel.Deployment, error) {
	p.calls++
	return nil, errorsForNativeProvider()
}
func errorsForNativeProvider() error { return service.ErrNativeRuntimeUnavailable }

func TestNativeStageRuntimeRegisteredIDORAndImportedFlowAreUnavailable(t *testing.T) {
	f := newNativeStatusRouteFixture(t, true)
	ctx := context.Background()
	st := f.s.handler.Store
	other := &model.Pipeline{ID: "other-native", Name: "Other", Version: 1, UpdatedAt: time.Now().UTC()}
	other.SetDefaults()
	if err := st.Pipelines.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	vstore := vercel.NewMemory()
	now := time.Now().UTC()
	record := &vercel.Record{Connection: vercel.Connection{ID: "import-connection", Identity: vercel.Identity{ID: "user_fixture", Scope: "personal"}, ProjectIDs: []string{"prj_fixture"}, Enabled: true, LastSuccessAt: &now},
		Snapshot: vercel.Snapshot{Projects: []vercel.Project{{ID: "prj_fixture", Name: "Imported"}}, Deployments: []vercel.Deployment{}}}
	if err := vstore.Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	providerBefore, err := vstore.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	provider := &nativeStatusProviderCounter{}
	f.s.handler.Vercel = &vercel.Service{Store: vstore, Codec: f.s.handler.Codec, API: provider}
	w := f.send(t, "viewer", "/api/v1/catalog/pipelines")
	var listed catalog.List[catalog.Pipeline]
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &listed) != nil {
		t.Fatal("real catalog fixture did not produce imported flow")
	}
	importID := ""
	for _, p := range listed.Items {
		if p.Management == "vercel" {
			importID = p.ID
		}
	}
	if importID == "" {
		t.Fatal("missing generated provider flow identity")
	}
	before := f.snapshot(t)
	for _, pipelineID := range []string{other.ID, importID} {
		path := strings.Replace(f.path(), "/"+f.p.ID+"/", "/"+pipelineID+"/", 1)
		w := f.send(t, "viewer", path)
		nativeRouteHeaders(t, w)
		var body map[string]string
		if w.Code != 404 || json.Unmarshal(w.Body.Bytes(), &body) != nil || !reflect.DeepEqual(body, map[string]string{"error": service.ErrNativeRuntimeNotFound.Error()}) {
			t.Fatal("foreign/imported identity did not return uniform native absence", w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(f.calls); !os.IsNotExist(err) {
		t.Fatal("foreign/imported identity inspected a workload")
	}
	if provider.calls != 0 || f.snapshot(t) != before {
		t.Fatal("foreign/imported read invoked provider or mutated native records")
	}
	after, err := vstore.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(providerBefore)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("native read modified imported provider snapshot")
	}
}
