package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

type nativeHTTPFixture struct {
	h          *Handler
	r          *gin.Engine
	p          *model.Pipeline
	run        *model.PipelineRun
	jobs       *jobqueue.Memory
	dir, calls string
}

// This fixture accepts the real enqueuer's immutable payload and advances only
// the in-memory queue lifecycle. Its recorded run and CLI replies are unit-test
// inputs; it does not execute a worker or claim a real Docker deployment.
func newNativeHTTPFixture(t *testing.T, stageType model.StageType) *nativeHTTPFixture {
	t.Helper()
	h := newTestHandler(t)
	f := &nativeHTTPFixture{h: h, jobs: jobqueue.NewMemory(), dir: t.TempDir()}
	f.p = &model.Pipeline{ID: "http-native", Name: "Native", Version: 1, UpdatedAt: time.Now().UTC(),
		Stages: []model.Stage{{ID: "deploy-web", Name: "Web", Type: stageType,
			Config: model.StageConfig{DeployRuntime: "docker", RuntimeName: "web", Image: "registry.invalid/web:v1"}}}}
	f.p.SetDefaults()
	ctx := context.Background()
	if err := h.Store.Pipelines.Create(ctx, f.p); err != nil {
		t.Fatal(err)
	}
	planned := &model.PipelineRun{ID: "http-run", PipelineID: f.p.ID, PipelineVersion: f.p.Version,
		Status: model.RunStatusPending, CreatedAt: time.Now().UTC(), Variables: map[string]string{},
		StartedByUserSub: "accepted-operator", StartedByEmail: "operator@fixture.invalid",
		StartedByGroups: []string{"operators"}, StartedByTokenHash: "accepted-actor-hash",
		StageRuns: []model.StageRun{{StageID: "deploy-web", Status: model.RunStatusPending}}}
	if err := service.NewJobQueueEnqueuer(f.jobs, h.Store.Pipelines).EnqueuePlannedRun(ctx, f.p, planned); err != nil {
		t.Fatal(err)
	}
	j, err := f.jobs.Dequeue(ctx, "unit-fixture", []string{service.JobKindPipelineRun})
	if err != nil || j == nil || j.ID != "run:"+planned.ID {
		t.Fatal("accepted queue fixture was not claimable", err)
	}
	if err = f.jobs.Complete(ctx, "unit-fixture", j.ID); err != nil {
		t.Fatal(err)
	}
	f.run = planned.Clone()
	start, finish := time.Now().Add(-2*time.Second).UTC(), time.Now().UTC()
	f.run.Status, f.run.StartedAt, f.run.FinishedAt = model.RunStatusSuccess, &start, &finish
	f.run.StageRuns[0].Status = model.RunStatusSuccess
	f.run.StageRuns[0].StartedAt, f.run.StageRuns[0].FinishedAt = &start, &finish
	f.run.StageRuns[0].Logs = "private-recorded-log-canary"
	f.run.StageRuns[0].RuntimeReceipt = &model.RuntimeReceipt{Version: 1, Runtime: "docker", RunID: f.run.ID, StageID: "deploy-web",
		ContainerID: strings.Repeat("a", 64), Name: "web", ImageReference: "registry.invalid/web:v1",
		ImageID: "sha256:" + strings.Repeat("b", 64), CapturedAt: start.Add(time.Second)}
	if err = h.Store.Runs.Create(ctx, f.run); err != nil {
		t.Fatal(err)
	}
	f.calls = filepath.Join(f.dir, "calls")
	cli := filepath.Join(f.dir, "docker-native-http")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$COOKER_NATIVE_HTTP_DIR/calls"
[ "$1" = container ] && [ "$2" = inspect ] && [ "$3" = --format ] || exit 91
if [ "$COOKER_NATIVE_HTTP_MODE" = fail ]; then printf '%s' private-cli-error-canary >&2; exit 44; fi
if [ "$COOKER_NATIVE_HTTP_MODE" = wait ]; then exec sleep 60; fi
cat "$COOKER_NATIVE_HTTP_DIR/response.json"
`
	if err = os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COOKER_NATIVE_HTTP_DIR", f.dir)
	t.Setenv("COOKER_NATIVE_HTTP_MODE", "")
	h.Runtime = &service.RuntimeService{Store: h.Store, DockerBin: cli}
	h.Jobs = f.jobs
	f.reply(t, f.run.StageRuns[0].RuntimeReceipt.ContainerID)
	f.r = gin.New()
	f.r.GET("/api/v1/pipelines/:id/runs/:runId/stages/:stageId/runtime", h.GetNativeStageRuntime)
	return f
}

func (f *nativeHTTPFixture) reply(t *testing.T, id string) {
	t.Helper()
	rr := f.run.StageRuns[0].RuntimeReceipt
	b, err := json.Marshal([]string{id, "/" + rr.Name, rr.ImageID, rr.ImageReference, rr.RunID, rr.StageID, "running", "configured", "healthy"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.dir, "response.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *nativeHTTPFixture) path() string {
	return "/api/v1/pipelines/" + f.p.ID + "/runs/" + f.run.ID + "/stages/deploy-web/runtime"
}

func (f *nativeHTTPFixture) get(t *testing.T, path string, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, strings.NewReader(`{"containerId":"caller-container","image":"caller-image","name":"caller-name"}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("native status response missing safe headers")
	}
	if strings.Contains(w.Body.String(), "canary") || strings.Contains(w.Body.String(), "accepted-actor-hash") {
		t.Fatal("private diagnostic or retained input escaped response")
	}
	return w
}

func nativeHTTPStatus(t *testing.T, w *httptest.ResponseRecorder) service.NativeRuntimeStatus {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatal("native status response", w.Code, w.Body.String())
	}
	var s service.NativeRuntimeStatus
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func nativeHTTPError(t *testing.T, w *httptest.ResponseRecorder, code int, fixed string) {
	t.Helper()
	var body map[string]string
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Code != code || !reflect.DeepEqual(body, map[string]string{"error": fixed}) {
		t.Fatal("error response is not fixed", w.Code, w.Body.String())
	}
}

func (f *nativeHTTPFixture) noInspect(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(f.calls); !os.IsNotExist(err) {
		t.Fatal("unavailable or foreign subject reached CLI")
	}
}

func (f *nativeHTTPFixture) snapshot(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	p, err := f.h.Store.Pipelines.List(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.h.Store.Runs.List(ctx, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	full, err := f.h.Store.Runs.Get(ctx, f.run.ID)
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
	apps, err := f.h.Store.Apps.List(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	history, err := f.h.Store.AppDeploys.ListByApp(ctx, "not-an-app", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal([]any{p, r, full, j, stats, apps, history})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNativeStageRuntimeHTTPAcceptedPathOnlyAndReadOnly(t *testing.T) {
	f := newNativeHTTPFixture(t, model.StageTypeDeploy)
	before := f.snapshot(t)
	s := nativeHTTPStatus(t, f.get(t, f.path()+"?containerId=foreign&image=foreign&name=foreign&pipelineId=foreign&runId=foreign&stageId=foreign", context.Background()))
	if s.Subject != (service.NativeRuntimeSubject{Kind: "native-stage", PipelineID: f.p.ID, RunID: f.run.ID, StageID: "deploy-web"}) || s.Binding != "verified" || s.RecordedStageStatus != model.RunStatusSuccess || s.State != "running" || s.Health != "healthy" || s.ContainerID != strings.Repeat("a", 64) || s.ObservedAt == nil || !reflect.DeepEqual(s.SupportedActions, []string{"inspect"}) {
		t.Fatal("typed exact observation not returned", s)
	}
	data, err := os.ReadFile(f.calls)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 5 || lines[0] != "container" || lines[1] != "inspect" || lines[2] != "--format" || lines[4] != s.ContainerID || !strings.Contains(lines[3], ".Config.Image") || !strings.Contains(lines[3], "cooker.run-id") || !strings.Contains(lines[3], "cooker.stage-id") || strings.Contains(lines[3], ".Config.Env") || strings.Contains(lines[3], ".State.Health.Log") || strings.Contains(lines[3], "{{json .Config.Labels}}") {
		t.Fatal("unsafe or caller-selected inspect arguments")
	}
	if after := f.snapshot(t); after != before {
		t.Fatal("status read changed native records or created application history")
	}
}

func TestNativeStageRuntimeHTTPOwnershipAndAcceptedDefinition(t *testing.T) {
	f := newNativeHTTPFixture(t, model.StageTypeDeploy)
	other := &model.Pipeline{ID: "other-native", Name: "Other", Version: 1, UpdatedAt: time.Now().UTC()}
	other.SetDefaults()
	if err := f.h.Store.Pipelines.Create(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	foreign := strings.Replace(f.path(), "/"+f.p.ID+"/", "/"+other.ID+"/", 1)
	nativeHTTPError(t, f.get(t, foreign, context.Background()), 404, service.ErrNativeRuntimeNotFound.Error())
	f.noInspect(t)
	current, err := f.h.Store.Pipelines.Get(context.Background(), f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Stages = []model.Stage{{ID: "replacement", Type: model.StageTypeApproval}}
	if err = f.h.Store.Pipelines.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	s := nativeHTTPStatus(t, f.get(t, f.path(), context.Background()))
	if s.Binding != "verified" || s.Name != "web" || s.ImageReference != "registry.invalid/web:v1" {
		t.Fatal("mutable definition replaced accepted runtime identity", s)
	}
	if err = os.Remove(f.calls); err != nil {
		t.Fatal(err)
	}
	if err = f.h.Store.Pipelines.Delete(context.Background(), f.p.ID); err != nil {
		t.Fatal(err)
	}
	nativeHTTPError(t, f.get(t, f.path(), context.Background()), 404, service.ErrNativeRuntimeNotFound.Error())
	f.noInspect(t)
}

func TestNativeStageRuntimeHTTPUnavailableDoesNotInspect(t *testing.T) {
	for _, scenario := range []string{"missing-job", "build", "approval", "failed-stage", "missing-receipt", "legacy", "capture-warning"} {
		t.Run(scenario, func(t *testing.T) {
			typeOfStage := model.StageTypeDeploy
			if scenario == "build" {
				typeOfStage = model.StageTypeBuild
			}
			if scenario == "approval" {
				typeOfStage = model.StageTypeApproval
			}
			f := newNativeHTTPFixture(t, typeOfStage)
			switch scenario {
			case "missing-job":
				f.h.Jobs = jobqueue.NewMemory()
			case "failed-stage":
				f.run.StageRuns[0].Status = model.RunStatusFailed
			case "missing-receipt":
				f.run.StageRuns[0].RuntimeReceipt = nil
			case "legacy":
				f.run.PipelineVersion = 0
			case "capture-warning":
				f.run.StageRuns[0].RuntimeBindingWarning = "runtime_binding_unavailable"
			}
			if err := f.h.Store.Runs.Update(context.Background(), f.run); err != nil {
				t.Fatal(err)
			}
			s := nativeHTTPStatus(t, f.get(t, f.path(), context.Background()))
			if s.Binding != "unavailable" || s.State != "unknown" || s.Health != "unknown" || s.ContainerID != "" || len(s.SupportedActions) != 0 {
				t.Fatal("unrecorded operation acquired runtime capability", s)
			}
			f.noInspect(t)
		})
	}
}

type nativeHTTPFailingPipelines struct{ store.PipelineStore }

func (s nativeHTTPFailingPipelines) Get(context.Context, string) (*model.Pipeline, error) {
	return nil, errors.New("private-store-error-canary")
}

type nativeHTTPFailingRuns struct{ store.RunStore }

func (s nativeHTTPFailingRuns) GetSummary(context.Context, string) (*model.PipelineRun, error) {
	return nil, errors.New("private-run-error-canary")
}

type nativeHTTPReadJobs struct {
	jobqueue.Store
	fail      bool
	malformed bool
}

func (s nativeHTTPReadJobs) Get(ctx context.Context, id string) (*jobqueue.Job, error) {
	if s.fail {
		return nil, errors.New("private-queue-error-canary")
	}
	j, err := s.Store.Get(ctx, id)
	if err == nil && s.malformed {
		j.Payload = []byte(`{"private":"private-payload-canary"`)
	}
	return j, err
}

func TestNativeStageRuntimeHTTPFixedErrorsAndPrivacy(t *testing.T) {
	for _, scenario := range []string{"nil-runtime", "nil-jobs", "store", "run-store", "queue", "invalid-id", "missing-pipeline", "missing-run", "missing-stage", "malformed-payload"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeHTTPFixture(t, model.StageTypeDeploy)
			path, code, fixed := f.path(), 503, service.ErrNativeRuntimeUnavailable.Error()
			switch scenario {
			case "nil-runtime":
				f.h.Runtime = nil
			case "nil-jobs":
				f.h.Jobs = nil
			case "store":
				f.h.Runtime.Store.Pipelines = nativeHTTPFailingPipelines{f.h.Store.Pipelines}
			case "run-store":
				f.h.Runtime.Store.Runs = nativeHTTPFailingRuns{f.h.Store.Runs}
			case "queue":
				f.h.Jobs = nativeHTTPReadJobs{Store: f.jobs, fail: true}
			case "invalid-id":
				path = strings.Replace(path, "/deploy-web/", "/bad%3Fstage/", 1)
				code, fixed = 400, service.ErrNativeRuntimeRequest.Error()
			case "missing-pipeline":
				path = strings.Replace(path, "/http-native/", "/missing/", 1)
				code, fixed = 404, service.ErrNativeRuntimeNotFound.Error()
			case "missing-run":
				path = strings.Replace(path, "/http-run/", "/missing/", 1)
				code, fixed = 404, service.ErrNativeRuntimeNotFound.Error()
			case "missing-stage":
				path = strings.Replace(path, "/deploy-web/", "/missing/", 1)
				code, fixed = 404, service.ErrNativeRuntimeNotFound.Error()
			case "malformed-payload":
				f.h.Jobs = nativeHTTPReadJobs{Store: f.jobs, malformed: true}
			}
			w := f.get(t, path, context.Background())
			if scenario == "malformed-payload" {
				if s := nativeHTTPStatus(t, w); s.Binding != "unavailable" || len(s.SupportedActions) != 0 {
					t.Fatal("malformed accepted payload acquired actions")
				}
			} else {
				nativeHTTPError(t, w, code, fixed)
			}
			f.noInspect(t)
		})
	}
}

func TestNativeStageRuntimeHTTPRuntimeLossRemainsUnavailable(t *testing.T) {
	for _, scenario := range []string{"absent-container", "replaced-id"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeHTTPFixture(t, model.StageTypeDeploy)
			if scenario == "absent-container" {
				t.Setenv("COOKER_NATIVE_HTTP_MODE", "fail")
			} else {
				f.reply(t, strings.Repeat("c", 64))
			}
			s := nativeHTTPStatus(t, f.get(t, f.path(), context.Background()))
			if s.Binding != "unavailable" || s.State != "unknown" || s.Health != "unknown" || s.ContainerID != "" || len(s.SupportedActions) != 0 || s.RecordedStageStatus != model.RunStatusSuccess {
				t.Fatal("missing current runtime changed recorded deployment outcome or exposed substituted identity", s)
			}
		})
	}
}

func TestNativeStageRuntimeHTTPRequestCancellationIsBounded(t *testing.T) {
	f := newNativeHTTPFixture(t, model.StageTypeDeploy)
	t.Setenv("COOKER_NATIVE_HTTP_MODE", "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	s := nativeHTTPStatus(t, f.get(t, f.path(), ctx))
	if time.Since(start) > 2*time.Second || ctx.Err() != context.DeadlineExceeded || s.Binding != "unavailable" || len(s.SupportedActions) != 0 {
		t.Fatal("request cancellation detached or prolonged runtime inspection", s)
	}
	if _, err := os.Stat(f.calls); err != nil {
		t.Fatal("cancellation fixture did not reach owned CLI", err)
	}
}
