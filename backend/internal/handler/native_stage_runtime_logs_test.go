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

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

// Real accepted queue snapshots from the status fixture; recorded receipts and
// the CLI are unit fixtures, without a worker or Docker deployment claim.
func newNativeLogsHTTPFixture(t *testing.T) *nativeHTTPFixture {
	t.Helper()
	f := newNativeHTTPFixture(t, model.StageTypeDeploy)
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$COOKER_NATIVE_HTTP_DIR/calls"
if [ "$COOKER_NATIVE_HTTP_MODE" = fail ]; then printf '%s' private-cli-error-canary >&2; exit 44; fi
if [ "$1" = container ] && [ "$2" = inspect ] && [ "$3" = --format ]; then cat "$COOKER_NATIVE_HTTP_DIR/response.json"; exit; fi
[ "$1" = logs ] || exit 91
printf '2026-10-05T00:00:00Z public marker one\n2026-10-05T00:00:01Z public marker two\n'
`
	if err := os.WriteFile(f.h.Runtime.DockerBin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/pipelines/:id/runs/:runId/stages/:stageId"
	f.r.GET(base+"/log-replicas", f.h.GetNativeStageLogReplicas)
	f.r.POST(base+"/log-sessions", f.h.OpenNativeStageLogSession)
	f.r.GET(base+"/log-sessions/:sessionId", f.h.GetNativeStageLogSession)
	f.r.GET(base+"/log-sessions/:sessionId/download", f.h.DownloadNativeStageLogSession)
	f.r.DELETE(base+"/log-sessions/:sessionId", f.h.CloseNativeStageLogSession)
	return f
}

func nativeLogsHTTP(t *testing.T, f *nativeHTTPFixture, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	f.r.ServeHTTP(w, req)
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing native log response headers")
	}
	if strings.Contains(w.Body.String(), "canary") || strings.Contains(w.Body.String(), "accepted-actor-hash") {
		t.Fatal("private retained input or diagnostic leaked")
	}
	return w
}
func nativeLogsHTTPBase(f *nativeHTTPFixture) string { return strings.TrimSuffix(f.path(), "/runtime") }

func TestNativeStageLogsHTTPReplicasSnapshotPagingDownloadAndClose(t *testing.T) {
	f := newNativeLogsHTTPFixture(t)
	base := nativeLogsHTTPBase(f)
	w := nativeLogsHTTP(t, f, http.MethodGet, base+"/log-replicas?containerId=foreign", "")
	var replicas service.NativeRuntimeLogReplicas
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replicas) != nil || len(replicas.Replicas) != 1 || replicas.Replicas[0].ContainerID != strings.Repeat("a", 64) || replicas.Subject.RunID != f.run.ID {
		t.Fatal("replica identity", w.Code, w.Body.String())
	}
	w = nativeLogsHTTP(t, f, http.MethodPost, base+"/log-sessions", `{"expectedRunId":"http-run","maxLines":20}`)
	var page service.NativeRuntimeLogPage
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.SessionID == "" || page.Total != 2 || page.RunID != f.run.ID || strings.Contains(w.Body.String(), `"appId"`) || strings.Contains(w.Body.String(), `"service"`) {
		t.Fatal("native session DTO", w.Code, w.Body.String())
	}
	session := base + "/log-sessions/" + page.SessionID
	w = nativeLogsHTTP(t, f, http.MethodGet, session+"?offset=1&limit=1", "")
	var second service.NativeRuntimeLogPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &second) != nil || len(second.Entries) != 1 || second.Entries[0] != page.Entries[1] {
		t.Fatal("stable native page")
	}
	w = nativeLogsHTTP(t, f, http.MethodGet, session+"/download", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "marker one") || !strings.Contains(w.Body.String(), "marker two") || w.Header().Get("Content-Disposition") != `attachment; filename="cooker-native-stage-logs.txt"` {
		t.Fatal("download", w.Code, w.Body.String())
	}
	for i := 0; i < 2; i++ {
		w = nativeLogsHTTP(t, f, http.MethodDelete, session, "")
		if w.Code != 204 {
			t.Fatal("idempotent close", w.Code, w.Body.String())
		}
	}
	w = nativeLogsHTTP(t, f, http.MethodGet, session, "")
	nativeHTTPError(t, w, 404, service.ErrRuntimeLogExpired.Error())
	data, err := os.ReadFile(filepath.Join(f.dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "foreign") || !strings.Contains(string(data), strings.Repeat("a", 64)) {
		t.Fatal("caller retargeted native container")
	}
}

func TestNativeStageLogsHTTPStrictOptionsAndCursorBounds(t *testing.T) {
	f := newNativeLogsHTTPFixture(t)
	base := nativeLogsHTTPBase(f)
	for _, body := range []string{`{"subject":{}}`, `{"maxLines":1} {}`, `{"maxLines":`, `[]`, `{"expectedRunId":"foreign"}`, `{"containerId":"foreign"}`, `{"search":"` + strings.Repeat("x", 17000) + `"}`} {
		w := nativeLogsHTTP(t, f, http.MethodPost, base+"/log-sessions", body)
		nativeHTTPError(t, w, 400, service.ErrRuntimeLogRequest.Error())
	}
	for _, query := range []string{"offset=-1", "offset=no", "offset=999999999999999999999999", "limit=0", "limit=501", "limit=no"} {
		w := nativeLogsHTTP(t, f, http.MethodGet, base+"/log-sessions/11111111-1111-1111-1111-111111111111?"+query, "")
		nativeHTTPError(t, w, 400, service.ErrRuntimeLogRequest.Error())
	}
}

func TestNativeStageLogsHTTPFixedErrorsAndUnavailableDependency(t *testing.T) {
	f := newNativeLogsHTTPFixture(t)
	base := nativeLogsHTTPBase(f)
	t.Setenv("COOKER_NATIVE_HTTP_MODE", "fail")
	w := nativeLogsHTTP(t, f, http.MethodGet, base+"/log-replicas", "")
	nativeHTTPError(t, w, 503, service.ErrRuntimeLogUnavailable.Error())
	f.h.Runtime = nil
	for _, item := range []struct{ method, suffix string }{{"GET", "/log-replicas"}, {"POST", "/log-sessions"}, {"GET", "/log-sessions/id"}, {"GET", "/log-sessions/id/download"}, {"DELETE", "/log-sessions/id"}} {
		w = nativeLogsHTTP(t, f, item.method, base+item.suffix, "{}")
		nativeHTTPError(t, w, 503, service.ErrRuntimeLogUnavailable.Error())
	}
	for _, tc := range []struct {
		err     error
		code    int
		message string
	}{{service.ErrRuntimeLogRequest, 400, service.ErrRuntimeLogRequest.Error()}, {service.ErrRuntimeLogExpired, 404, service.ErrRuntimeLogExpired.Error()}, {service.ErrRuntimeLogCapacity, 429, service.ErrRuntimeLogCapacity.Error()}, {errors.New("private-handler-error-canary"), 503, service.ErrRuntimeLogUnavailable.Error()}} {
		w = httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		nativeLogHeaders(c)
		runtimeLogHTTPError(c, tc.err)
		nativeHTTPError(t, w, tc.code, tc.message)
	}
	channel, produce, err := f.h.PrepareNativeStageLogs(context.Background(), service.NativeRuntimeSubject{})
	if channel != "" || produce != nil || !errors.Is(err, service.ErrRuntimeLogUnavailable) {
		t.Fatal("missing native WS runtime must be fixed unavailable")
	}
}

func TestNativeStageLogsHTTPSubjectOwnershipAndReadOnlyRecords(t *testing.T) {
	f := newNativeLogsHTTPFixture(t)
	base := nativeLogsHTTPBase(f)
	ctx := context.Background()
	beforeRun, _ := f.h.Store.Runs.Get(ctx, f.run.ID)
	beforeJob, _ := f.jobs.Get(ctx, "run:"+f.run.ID)
	beforeApps, _ := f.h.Store.Apps.List(ctx, 0, 0)
	for _, path := range []string{strings.Replace(base, f.p.ID, "foreign", 1), strings.Replace(base, "deploy-web", "foreign", 1), strings.Replace(base, f.p.ID, "vflow_imported", 1)} {
		w := nativeLogsHTTP(t, f, "GET", path+"/log-replicas", "")
		if w.Code != 404 && w.Code != 503 {
			t.Fatal("foreign subject was admitted", w.Code, w.Body.String())
		}
	}
	w := nativeLogsHTTP(t, f, "POST", base+"/log-sessions", `{}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	afterRun, _ := f.h.Store.Runs.Get(ctx, f.run.ID)
	afterJob, _ := f.jobs.Get(ctx, "run:"+f.run.ID)
	afterApps, _ := f.h.Store.Apps.List(ctx, 0, 0)
	if !reflect.DeepEqual(beforeRun, afterRun) || !reflect.DeepEqual(beforeJob, afterJob) || !reflect.DeepEqual(beforeApps, afterApps) {
		t.Fatal("log observation mutated business records")
	}
}
