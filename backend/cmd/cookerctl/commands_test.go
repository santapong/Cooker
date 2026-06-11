package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// runArgs invokes the dispatcher with COOKER_URL/COOKER_TOKEN derived from
// the given server + token, capturing stdout/stderr and returning the exit
// code. Global flags are passed explicitly so the test doesn't mutate env.
func runArgs(srv *httptest.Server, token string, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	full := append([]string{"--server", srv.URL, "--token", token}, args...)
	code = run(full, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRun_NoArgs_PrintsUsage(t *testing.T) {
	var out, errb bytes.Buffer
	code := run(nil, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "cookerctl") {
		t.Fatalf("usage not printed: %q", out.String())
	}
}

func TestRun_UnknownCommand_Exit2(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"frobnicate"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestPipelinesList_Table(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"p1","name":"alpha","version":3,"stages":[{"id":"s1"}]}]`))
	}))
	defer srv.Close()

	code, out, errb := runArgs(srv, "ck_tok", "pipelines", "list")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errb)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "p1") {
		t.Fatalf("table missing pipeline: %q", out)
	}
	if !strings.Contains(out, "NAME") {
		t.Fatalf("table header missing: %q", out)
	}
}

func TestPipelinesList_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"p1","name":"alpha"}]`))
	}))
	defer srv.Close()

	code, out, _ := runArgs(srv, "ck_tok", "--json", "pipelines", "list")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, `"id": "p1"`) {
		t.Fatalf("json output missing id: %q", out)
	}
}

func TestPipelinesList_Unauthorized_PrintsHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"authentication failed"}`))
	}))
	defer srv.Close()

	code, _, errb := runArgs(srv, "ck_bad", "pipelines", "list")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb, "Settings → API tokens") {
		t.Fatalf("401 hint not printed: %q", errb)
	}
}

func TestRequireToken_MissingToken_Exit1(t *testing.T) {
	var out, errb bytes.Buffer
	// No --token and (in CI) no COOKER_TOKEN: the guard must fire before
	// any network call. Pass an explicit empty token to be deterministic.
	code := run([]string{"--server", "http://127.0.0.1:0", "--token", "", "pipelines", "list"}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "no API token") {
		t.Fatalf("missing-token message absent: %q", errb.String())
	}
}

// TestPipelinesRun_Follow_Success scripts a run that progresses
// pending → running → success across polls, with the single stage going
// running → success. It asserts the follow loop prints both transitions,
// fetches the stage logs on completion, prints the terminal line, and
// exits 0.
func TestPipelinesRun_Follow_Success(t *testing.T) {
	var poll int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/pipelines/p1/run":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"run1","pipelineId":"p1","status":"pending","stageRuns":[{"stageId":"s1","status":"pending"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pipelines/p1":
			_, _ = w.Write([]byte(`{"id":"p1","name":"alpha","stages":[{"id":"s1","name":"Build"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pipelines/p1/runs/run1":
			// Scripted state machine: each poll advances one step.
			n := atomic.AddInt32(&poll, 1)
			switch n {
			case 1:
				_, _ = w.Write([]byte(`{"id":"run1","pipelineId":"p1","status":"running","stageRuns":[{"stageId":"s1","status":"running"}]}`))
			default:
				_, _ = w.Write([]byte(`{"id":"run1","pipelineId":"p1","status":"success","stageRuns":[{"stageId":"s1","status":"success"}]}`))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pipelines/p1/runs/run1/logs/s1":
			_, _ = w.Write([]byte(`{"logs":"compiling...\ndone\n"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	code, out, errb := runArgs(srv, "ck_tok", "pipelines", "run", "p1", "--follow")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errb)
	}
	for _, want := range []string{
		"Run run1 started",
		"Build → running",
		"Build → success",
		"logs: Build",
		"compiling...",
		"finished: success",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("follow output missing %q in:\n%s", want, out)
		}
	}
}

// TestPipelinesRun_Follow_Failure asserts the exit code mirrors a failed
// run (exit 1) and that the failing stage's logs are still printed.
func TestPipelinesRun_Follow_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/run"):
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"run1","pipelineId":"p1","status":"pending","stageRuns":[{"stageId":"s1","status":"pending"}]}`))
		case r.URL.Path == "/api/v1/pipelines/p1":
			_, _ = w.Write([]byte(`{"id":"p1","name":"alpha","stages":[{"id":"s1","name":"Build"}]}`))
		case strings.HasSuffix(r.URL.Path, "/runs/run1"):
			_, _ = w.Write([]byte(`{"id":"run1","pipelineId":"p1","status":"failed","stageRuns":[{"stageId":"s1","status":"failed"}]}`))
		case strings.HasSuffix(r.URL.Path, "/logs/s1"):
			_, _ = w.Write([]byte(`{"logs":"boom\n"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	code, out, _ := runArgs(srv, "ck_tok", "pipelines", "run", "p1", "--follow")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for failed run", code)
	}
	if !strings.Contains(out, "finished: failed") {
		t.Fatalf("missing terminal failure line: %q", out)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("failing stage logs not printed: %q", out)
	}
}

func TestRunsLogs_AllStages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/runs/run1"):
			_, _ = w.Write([]byte(`{"id":"run1","pipelineId":"p1","status":"success","stageRuns":[{"stageId":"s1","status":"success"},{"stageId":"s2","status":"success"}]}`))
		case r.URL.Path == "/api/v1/pipelines/p1":
			_, _ = w.Write([]byte(`{"id":"p1","name":"alpha","stages":[{"id":"s1","name":"Build"},{"id":"s2","name":"Deploy"}]}`))
		case strings.HasSuffix(r.URL.Path, "/logs/s1"):
			_, _ = w.Write([]byte(`{"logs":"build log"}`))
		case strings.HasSuffix(r.URL.Path, "/logs/s2"):
			_, _ = w.Write([]byte(`{"logs":"deploy log"}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	code, out, errb := runArgs(srv, "ck_tok", "runs", "logs", "run1", "--pipeline", "p1")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errb)
	}
	for _, want := range []string{"Build", "build log", "Deploy", "deploy log"} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs output missing %q: %q", want, out)
		}
	}
}

func TestRunsLogs_MissingPipelineFlag_Exit2(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	code, _, errb := runArgs(srv, "ck_tok", "runs", "logs", "run1")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb, "--pipeline") {
		t.Fatalf("usage hint missing: %q", errb)
	}
}

func TestPipelinesExport_ToStdout(t *testing.T) {
	const yamlDoc = "apiVersion: cooker.dev/v1\nkind: Pipeline\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = fmt.Fprint(w, yamlDoc)
	}))
	defer srv.Close()

	code, out, errb := runArgs(srv, "ck_tok", "pipelines", "export", "p1")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errb)
	}
	if out != yamlDoc {
		t.Fatalf("export stdout = %q, want %q", out, yamlDoc)
	}
}

func TestParseGlobal_EnvFallback(t *testing.T) {
	t.Setenv("COOKER_URL", "http://example:9999")
	t.Setenv("COOKER_TOKEN", "ck_fromenv")
	cfg, rest, err := parseGlobal([]string{"pipelines", "list"})
	if err != nil {
		t.Fatalf("parseGlobal: %v", err)
	}
	if cfg.server != "http://example:9999" {
		t.Fatalf("server = %q, want env value", cfg.server)
	}
	if cfg.token != "ck_fromenv" {
		t.Fatalf("token = %q, want env value", cfg.token)
	}
	if len(rest) != 2 || rest[0] != "pipelines" {
		t.Fatalf("rest = %v", rest)
	}
}

func TestParseGlobal_FlagOverridesEnv(t *testing.T) {
	t.Setenv("COOKER_URL", "http://env:1111")
	cfg, _, err := parseGlobal([]string{"--server", "http://flag:2222", "version"})
	if err != nil {
		t.Fatalf("parseGlobal: %v", err)
	}
	if cfg.server != "http://flag:2222" {
		t.Fatalf("flag did not override env: server = %q", cfg.server)
	}
}
