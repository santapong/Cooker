package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/santapong/cooker/internal/catalog"
	"github.com/santapong/cooker/internal/provider/vercel"
	"github.com/santapong/cooker/internal/service"
)

const nativeLogRouteBase = "/api/v1/pipelines/:id/runs/:runId/stages/:stageId"

func nativeRegisteredRequest(t *testing.T, f *nativeStatusRouteFixture, role, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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
		t.Fatal("private native input escaped route")
	}
	return w
}

func newNativeLogsRouteFixture(t *testing.T, hub bool) *nativeStatusRouteFixture {
	t.Helper()
	f := newNativeStatusRouteFixture(t, true)
	f.s.wsTickets = newWSTicketStore(time.Minute)
	// The genuine enqueuer supplies accepted authority; the successful run and
	// full-ID receipt remain unit seeds and this executable is an owned CLI stub.
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$COOKER_NATIVE_ROUTE_DIR/calls"
if [ "$COOKER_NATIVE_ROUTE_MODE" = fail ]; then printf '%s' private-native-route-canary >&2; exit 44; fi
if [ "$1" = container ] && [ "$2" = inspect ] && [ "$3" = --format ]; then cat "$COOKER_NATIVE_ROUTE_DIR/response.json"; exit; fi
[ "$1" = logs ] || exit 91
if [ "$2" = --follow ]; then printf 'native live marker\n'; exec sleep 60; fi
printf '2026-10-05T00:00:00Z native snapshot marker\n'
`
	if err := os.WriteFile(f.s.handler.Runtime.DockerBin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COOKER_NATIVE_ROUTE_MODE", "")
	if hub {
		f.s.wsHub = NewWebSocketHub([]string{"http://native.fixture"})
		done := make(chan struct{})
		go func() { defer close(done); f.s.wsHub.Run() }()
		t.Cleanup(func() { _ = f.s.wsHub.Close(); runtimeWait(t, done, "native route hub") })
	}
	return f
}

func nativeRegisteredBase(f *nativeStatusRouteFixture) string {
	return strings.TrimSuffix(f.path(), "/runtime")
}
func nativeRegisteredWSPath(f *nativeStatusRouteFixture) string {
	return "/ws/pipelines/" + f.p.ID + "/runs/" + f.run.ID + "/stages/deploy-web/runtime-logs"
}

func nativeRegisteredTicket(t *testing.T, f *nativeStatusRouteFixture, role, path string) string {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"path": path})
	w := nativeRegisteredRequest(t, f, role, "POST", "/api/v1/ws-tickets", string(b))
	if w.Code != 200 {
		t.Fatal("registered native ticket mint", w.Code, w.Body.String())
	}
	var result struct {
		Ticket string `json:"ticket"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Ticket == "" {
		t.Fatal("missing registered ticket")
	}
	return result.Ticket
}

func TestNativeRuntimeLogsRegisteredHTTPAuthAndLogRoles(t *testing.T) {
	f := newNativeStatusRouteFixture(t, false)
	base := "/api/v1/pipelines/p/runs/r/stages/s"
	endpoints := []struct{ method, suffix string }{{"GET", "/log-replicas"}, {"POST", "/log-sessions"}, {"GET", "/log-sessions/11111111-1111-1111-1111-111111111111"}, {"DELETE", "/log-sessions/11111111-1111-1111-1111-111111111111"}, {"GET", "/log-sessions/11111111-1111-1111-1111-111111111111/download"}}
	for _, role := range []string{"", "viewer", "approver", "operator", "admin"} {
		for _, endpoint := range endpoints {
			w := nativeRegisteredRequest(t, f, role, endpoint.method, base+endpoint.suffix, "{}")
			want := 503
			if role == "" {
				want = 401
			} else if role == "viewer" || role == "approver" {
				want = 403
			}
			if w.Code != want {
				t.Fatal("native HTTP role", role, endpoint, w.Code, w.Body.String())
			}
			nativeRouteHeaders(t, w)
		}
	}
	// Log authorization does not change read-only runtime status.
	for _, role := range []string{"viewer", "approver"} {
		w := f.send(t, role, base+"/runtime")
		if w.Code != 503 {
			t.Fatal("status stopped being viewer-readable")
		}
	}
	counts := map[string]int{}
	for _, route := range f.s.router.Routes() {
		if strings.HasPrefix(route.Path, nativeLogRouteBase+"/log-") {
			counts[route.Method+" "+route.Path]++
		}
	}
	if len(counts) != 5 {
		t.Fatal("unexpected native log routes", counts)
	}
	for _, n := range counts {
		if n != 1 {
			t.Fatal("duplicate native log endpoint")
		}
	}
}

func TestNativeRuntimeLogsRegisteredSessionsReadOnlyAndSubjectBound(t *testing.T) {
	f := newNativeLogsRouteFixture(t, false)
	base := nativeRegisteredBase(f)
	before := f.snapshot(t)
	w := nativeRegisteredRequest(t, f, "operator", "GET", base+"/log-replicas", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	nativeRouteHeaders(t, w)
	w = nativeRegisteredRequest(t, f, "admin", "POST", base+"/log-sessions", `{"expectedRunId":"route-run"}`)
	var page service.NativeRuntimeLogPage
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Total != 1 || page.Subject.PipelineID != f.p.ID || page.ContainerID != strings.Repeat("a", 64) {
		t.Fatal("registered native session", w.Code, w.Body.String())
	}
	session := base + "/log-sessions/" + page.SessionID
	for _, suffix := range []string{"", "/download"} {
		w = nativeRegisteredRequest(t, f, "operator", "GET", session+suffix, "")
		if w.Code != 200 {
			t.Fatal("registered cached logs", w.Code, w.Body.String())
		}
		nativeRouteHeaders(t, w)
	}
	for _, foreign := range []string{strings.Replace(base, f.p.ID, "foreign", 1), strings.Replace(base, "deploy-web", "foreign", 1), strings.Replace(base, f.p.ID, "vflow_imported", 1)} {
		w = nativeRegisteredRequest(t, f, "operator", "GET", foreign+"/log-sessions/"+page.SessionID, "")
		if w.Code != 404 {
			t.Fatal("foreign session read", w.Code, w.Body.String())
		}
		w = nativeRegisteredRequest(t, f, "operator", "DELETE", foreign+"/log-sessions/"+page.SessionID, "")
		if w.Code != 404 && w.Code != 204 {
			t.Fatal("foreign typed close", w.Code, w.Body.String())
		}
	}
	w = nativeRegisteredRequest(t, f, "operator", "GET", session, "")
	if w.Code != 200 {
		t.Fatal("foreign close removed native session")
	}
	for i := 0; i < 2; i++ {
		w = nativeRegisteredRequest(t, f, "admin", "DELETE", session, "")
		if w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w = nativeRegisteredRequest(t, f, "admin", "GET", session, "")
	if w.Code != 404 {
		t.Fatal("closed session retained")
	}
	if f.snapshot(t) != before {
		t.Fatal("native log routes changed business records")
	}
}

func TestNativeRuntimeLogsRegisteredRefusesImportedAndPrivateCLIError(t *testing.T) {
	f := newNativeLogsRouteFixture(t, false)
	base := nativeRegisteredBase(f)
	before := f.snapshot(t)
	ctx := context.Background()
	vstore := vercel.NewMemory()
	now := time.Now().UTC()
	record := &vercel.Record{Connection: vercel.Connection{ID: "native-log-import", Identity: vercel.Identity{ID: "user_fixture", Scope: "personal"}, ProjectIDs: []string{"prj_fixture"}, Enabled: true, LastSuccessAt: &now}, Snapshot: vercel.Snapshot{Projects: []vercel.Project{{ID: "prj_fixture", Name: "Imported"}}, Deployments: []vercel.Deployment{}}}
	if err := vstore.Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	providerBefore, err := vstore.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	provider := &nativeStatusProviderCounter{}
	f.s.handler.Vercel = &vercel.Service{Store: vstore, Codec: f.s.handler.Codec, API: provider}
	listedResponse := nativeRegisteredRequest(t, f, "viewer", "GET", "/api/v1/catalog/pipelines", "")
	var listed catalog.List[catalog.Pipeline]
	if listedResponse.Code != 200 || json.Unmarshal(listedResponse.Body.Bytes(), &listed) != nil {
		t.Fatal("catalog fixture failed")
	}
	importID := ""
	for _, p := range listed.Items {
		if p.Management == "vercel" {
			importID = p.ID
		}
	}
	if importID == "" {
		t.Fatal("catalog did not report actual imported flow")
	}
	for _, path := range []string{strings.Replace(base, f.p.ID, importID, 1), strings.Replace(base, "deploy-web", "foreign", 1)} {
		w := nativeRegisteredRequest(t, f, "operator", "GET", path+"/log-replicas", "")
		if w.Code != 404 && w.Code != 503 {
			t.Fatal("import or foreign stage admitted")
		}
	}
	t.Setenv("COOKER_NATIVE_ROUTE_MODE", "fail")
	w := nativeRegisteredRequest(t, f, "operator", "POST", base+"/log-sessions", `{}`)
	var body map[string]string
	if w.Code != 503 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body["error"] != service.ErrRuntimeLogUnavailable.Error() {
		t.Fatal("raw CLI error response", w.Code, w.Body.String())
	}
	if f.snapshot(t) != before || provider.calls != 0 {
		t.Fatal("failed log read wrote business records or invoked provider")
	}
	providerAfter, err := vstore.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeProvider, _ := json.Marshal(providerBefore)
	afterProvider, _ := json.Marshal(providerAfter)
	if string(beforeProvider) != string(afterProvider) {
		t.Fatal("native logs changed provider snapshot")
	}
}

func TestNativeRuntimeLogsRegisteredWSAdmissionOriginAndSingleUse(t *testing.T) {
	f := newNativeLogsRouteFixture(t, true)
	srv := httptest.NewServer(f.s.router)
	defer srv.Close()
	path := nativeRegisteredWSPath(f)
	before := f.snapshot(t)
	for _, role := range []string{"viewer", "approver"} {
		b, _ := json.Marshal(map[string]string{"path": path})
		w := nativeRegisteredRequest(t, f, role, "POST", "/api/v1/ws-tickets", string(b))
		if w.Code != 403 {
			t.Fatal("reader obtained native log ticket")
		}
	}
	dial := func(ticket, origin string) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+path+"?ticket="+url.QueryEscape(ticket), http.Header{"Origin": {origin}})
	}
	ticket := nativeRegisteredTicket(t, f, "operator", path)
	conn, response, err := dial(ticket, "http://wrong.fixture")
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != 403 {
		t.Fatal("native wrong origin admitted")
	}
	_ = response.Body.Close()
	_, response, err = dial(ticket, "http://native.fixture")
	if err == nil || response == nil || response.StatusCode != 401 {
		t.Fatal("refused-origin ticket reused")
	}
	_ = response.Body.Close()
	ticket = nativeRegisteredTicket(t, f, "admin", path)
	conn, response, err = dial(ticket, "http://native.fixture")
	if err != nil {
		t.Fatal("native registered upgrade", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, frame, err := conn.ReadMessage()
	if err != nil || string(frame) != "native live marker\n" {
		t.Fatal("native live producer failed after handler return", err)
	}
	_ = conn.Close()
	// Before upgrade, changed exact inspect identity refuses the producer.
	original, err := os.ReadFile(filepath.Join(f.dir, "response.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	if json.Unmarshal(original, &fields) != nil {
		t.Fatal("fixture inspect")
	}
	fields[0] = strings.Repeat("c", 64)
	changed, _ := json.Marshal(fields)
	if err = os.WriteFile(filepath.Join(f.dir, "response.json"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	ticket = nativeRegisteredTicket(t, f, "operator", path)
	conn, response, err = dial(ticket, "http://native.fixture")
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != 503 {
		t.Fatal("substituted identity upgraded")
	}
	_ = response.Body.Close()
	if f.snapshot(t) != before {
		t.Fatal("WS observation mutated business records")
	}
}

func TestNativeRuntimeLogsRegisteredWSMissingDependenciesAreFixed(t *testing.T) {
	for _, missing := range []string{"hub", "runtime"} {
		t.Run(missing, func(t *testing.T) {
			f := newNativeLogsRouteFixture(t, missing == "runtime")
			path := nativeRegisteredWSPath(f)
			if missing == "runtime" {
				f.s.handler.Runtime = nil
			}
			ticket := nativeRegisteredTicket(t, f, "operator", path)
			w := httptest.NewRecorder()
			f.s.router.ServeHTTP(w, httptest.NewRequest("GET", path+"?ticket="+url.QueryEscape(ticket), nil))
			nativeRouteHeaders(t, w)
			var body map[string]string
			if w.Code != 503 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body["error"] != service.ErrRuntimeLogUnavailable.Error() {
				t.Fatal("missing native dependency was not fixed unavailable", w.Code, w.Body.String())
			}
		})
	}
}

func TestNativeRuntimeLogsConnectionNonceIsolationAndLifetime(t *testing.T) {
	for _, ending := range []string{"disconnect", "hub-close"} {
		t.Run(ending, func(t *testing.T) {
			const origin = "http://native-lifetime.fixture"
			h := NewWebSocketHub([]string{origin})
			done := make(chan struct{})
			go func() { defer close(done); h.Run() }()
			var mu sync.Mutex
			returned := make([]chan struct{}, 2)
			stopped := make([]chan struct{}, 2)
			for i := 0; i < 2; i++ {
				returned[i] = make(chan struct{})
				stopped[i] = make(chan struct{})
			}
			contexts := make(chan context.Context, 2)
			sequence := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				index := sequence
				sequence++
				mu.Unlock()
				if index >= 2 {
					http.Error(w, "fixture request capacity", 429)
					return
				}
				requestDone := r.Context().Done()
				r = r.WithContext(context.WithValue(r.Context(), runtimeRequestIdentityKey{}, "native-request-value"))
				h.handleNativeRuntimeLogs(w, r, "native-runtime-logs:aW5qZWN0aXZl", func(ctx context.Context, out io.Writer) error {
					defer close(stopped[index])
					<-requestDone
					if ctx.Err() != nil || ctx.Value(runtimeRequestIdentityKey{}) != "native-request-value" {
						return errors.New("connection lost request values or lifetime")
					}
					contexts <- ctx
					_, err := io.WriteString(out, []string{"first connection\n", "second connection\n"}[index])
					if err != nil {
						return err
					}
					<-ctx.Done()
					return ctx.Err()
				})
				close(returned[index])
			}))
			connections := make([]*websocket.Conn, 2)
			t.Cleanup(func() {
				for _, conn := range connections {
					if conn != nil {
						_ = conn.Close()
					}
				}
				_ = h.Close()
				srv.Close()
				runtimeWait(t, done, "native lifetime hub")
			})
			for i := 0; i < 2; i++ {
				conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": {origin}})
				if err != nil {
					t.Fatal(err)
				}
				connections[i] = conn
				runtimeWait(t, returned[i], "native handler")
				_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				_, frame, err := conn.ReadMessage()
				if err != nil || string(frame) != []string{"first connection\n", "second connection\n"}[i] {
					t.Fatal("connection frame was mixed", i, err)
				}
			}
			h.mu.RLock()
			channels := make([]string, 0, 2)
			for channel, clients := range h.clients {
				if len(clients) != 1 {
					t.Error("shared native per-connection channel")
				}
				channels = append(channels, channel)
			}
			h.mu.RUnlock()
			if len(channels) != 2 || channels[0] == channels[1] {
				t.Fatal("native channels collided", len(channels))
			}
			if ending == "disconnect" {
				_ = connections[0].Close()
				runtimeWait(t, stopped[0], "first native producer")
				select {
				case <-stopped[1]:
					t.Fatal("first disconnect cancelled other producer")
				default:
				}
				_ = connections[1].Close()
			} else {
				_ = h.Close()
			}
			for i := 0; i < 2; i++ {
				runtimeWait(t, stopped[i], "native producer")
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				h.mu.RLock()
				remaining := len(h.clients)
				h.mu.RUnlock()
				if remaining == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("empty private nonce channels remain after disconnect")
				}
				time.Sleep(5 * time.Millisecond)
			}
			for i := 0; i < 2; i++ {
				select {
				case ctx := <-contexts:
					if ctx.Err() != context.Canceled {
						t.Fatal("native connection did not cancel")
					}
				default:
					t.Fatal("producer did not retain connection context")
				}
			}
		})
	}
}

func TestNativeRuntimeLogsConnectionChannelsAreInjective(t *testing.T) {
	seen := map[string]bool{}
	for _, base := range []string{"native-runtime-logs:YWJj", "native-runtime-logs:YWJjZA"} {
		for i := 0; i < 8; i++ {
			channel, err := nativeRuntimeConnectionChannel(base)
			if err != nil || !strings.HasPrefix(channel, base+":connection:") || seen[channel] {
				t.Fatal("native connection identity collision")
			}
			seen[channel] = true
		}
	}
}

type nativeFixedDiagnosticWriter struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *nativeFixedDiagnosticWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}
func (w *nativeFixedDiagnosticWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

type nativeFailingLogBackend struct {
	frames chan BroadcastMessage
	once   sync.Once
}

func (b *nativeFailingLogBackend) Publish(BroadcastMessage) error {
	return errors.New("private-native-publish-canary")
}
func (b *nativeFailingLogBackend) Subscribe() <-chan BroadcastMessage { return b.frames }
func (b *nativeFailingLogBackend) Close() error                       { b.once.Do(func() { close(b.frames) }); return nil }

func TestNativeRuntimeLogsFixedProducerAndPublicationDiagnostics(t *testing.T) {
	writer := &nativeFixedDiagnosticWriter{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(writer, nil)))
	defer slog.SetDefault(old)
	backend := &nativeFailingLogBackend{frames: make(chan BroadcastMessage)}
	h := NewWebSocketHubWithBackend([]string{"http://native-diagnostic.fixture"}, backend)
	done := make(chan struct{})
	go func() { defer close(done); h.Run() }()
	contextReady := make(chan context.Context, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleNativeRuntimeLogs(w, r, "native-runtime-logs:private-channel-canary", func(ctx context.Context, out io.Writer) error {
			contextReady <- ctx
			_, _ = io.WriteString(out, "public native frame\n")
			return errors.New("private-native-producer-canary")
		})
	}))
	var conn *websocket.Conn
	t.Cleanup(func() {
		if conn != nil {
			_ = conn.Close()
		}
		_ = h.Close()
		srv.Close()
		runtimeWait(t, done, "native diagnostic hub")
	})
	var err error
	conn, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": {"http://native-diagnostic.fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ctx := <-contextReady:
		runtimeWait(t, ctx.Done(), "native diagnostic producer")
	case <-time.After(3 * time.Second):
		t.Fatal("native diagnostic producer missing")
	}
	output := writer.String()
	if strings.Contains(output, "canary") || strings.Contains(output, "channel") || strings.Contains(output, `"err"`) || !strings.Contains(output, "native runtime logs publication unavailable") || !strings.Contains(output, "native runtime logs producer ended") {
		t.Fatal("native diagnostic was not fixed safe text")
	}
}
