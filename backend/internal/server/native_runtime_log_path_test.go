package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/service"
)

func TestNativeRuntimeLogCanonicalPathDecodesExactlyOnce(t *testing.T) {
	for _, ids := range [][3]string{{"pipeline:one", "run:two", "stage:three"}, {"โครงการ", "รัน", "ขั้นตอน"}, {"literal%2F", "literal%3F", "literal%25"}, {"a%b", "c%d", "e%f"}} {
		decoded := "/ws/pipelines/" + ids[0] + "/runs/" + ids[1] + "/stages/" + ids[2] + "/runtime-logs"
		escaped := "/ws/pipelines/" + url.PathEscape(ids[0]) + "/runs/" + url.PathEscape(ids[1]) + "/stages/" + url.PathEscape(ids[2]) + "/runtime-logs"
		got, ok := canonicalNativeRuntimeLogPath(escaped)
		subject, valid := nativeRuntimeLogSubject(got)
		if !ok || !valid || got != decoded || subject != (service.NativeRuntimeSubject{Kind: "native-stage", PipelineID: ids[0], RunID: ids[1], StageID: ids[2]}) || !matchWSPath(got) {
			t.Fatal("decoded native identity changed", ids, got)
		}
		req := httptest.NewRequest("GET", escaped, nil)
		if req.URL.Path != got {
			t.Fatal("mint canonical path differs from actual URL.Path")
		}
	}
	literal, ok := canonicalNativeRuntimeLogPath("/ws/pipelines/literal%252F/runs/r/stages/s/runtime-logs")
	if !ok || !strings.Contains(literal, "literal%2F") {
		t.Fatal("literal percent was decoded twice")
	}
	if _, ok := nativeRuntimeLogSubject(strings.Replace(literal, "%2F", "/", 1)); ok {
		t.Fatal("second-decoded separator was accepted")
	}
}

func TestNativeRuntimeLogCanonicalPathRejectsMalformedIdentities(t *testing.T) {
	base := "/ws/pipelines/p/runs/r/stages/s/runtime-logs"
	bad := []string{base + "/", base + "?ticket=x", base + "#fragment", strings.Replace(base, "/p/", "//", 1), "//" + strings.TrimPrefix(base, "/"), strings.Replace(base, "/runs/", "/jobs/", 1), strings.Replace(base, "/p/", "/%/", 1)}
	for _, id := range []string{"a%2Fb", "a%5Cb", "a%3Fb", "a%23b", "a%00b", "a%0Ab", "%20a", "a%20", strings.Repeat("x", 129)} {
		bad = append(bad, strings.Replace(base, "/p/", "/"+id+"/", 1))
	}
	for _, path := range bad {
		if _, ok := canonicalNativeRuntimeLogPath(path); ok {
			t.Fatal("malformed native mint path accepted", path)
		}
	}
	// Existing query-stripping behavior is deliberately unchanged outside the
	// new native prefix; a literal percent in old routes remains unnormalized.
	s := newMintServer()
	claims := &auth.Claims{Subject: "operator", Roles: []string{"operator"}}
	w := mintRequest(t, s, claims, `{"path":"/ws/runtime/app%25/service/logs?ignored=true"}`)
	if w.Code != 200 {
		t.Fatal("old App normalization changed", w.Code, w.Body.String())
	}
	var reply struct {
		Ticket string `json:"ticket"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &reply)
	grant, ok := s.wsTickets.Consume(reply.Ticket)
	if !ok || grant.Path != "/ws/runtime/app%25/service/logs" {
		t.Fatal("old App path was decoded")
	}
}

func TestNativeRuntimeLogTicketMintRolesAndDecodedGrant(t *testing.T) {
	for _, role := range []string{"viewer", "approver", "operator", "admin"} {
		s := newMintServer()
		claims := &auth.Claims{Subject: role, Roles: []string{role}}
		w := mintRequest(t, s, claims, `{"path":"/ws/pipelines/p%253F/runs/r%3Aone/stages/%E0%B8%A3%E0%B8%B1%E0%B8%99/runtime-logs"}`)
		want := 403
		if role == "operator" || role == "admin" {
			want = 200
		}
		if w.Code != want {
			t.Fatal("native mint role", role, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("native ticket response missing privacy headers")
		}
		if want == 200 {
			var reply struct {
				Ticket string `json:"ticket"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &reply)
			grant, ok := s.wsTickets.Consume(reply.Ticket)
			if !ok || grant.Subject != role || grant.Path != "/ws/pipelines/p%3F/runs/r:one/stages/รัน/runtime-logs" || !reflect.DeepEqual(grant.Roles, []string{role}) {
				t.Fatal("native ticket identity changed", grant)
			}
			if _, ok = s.wsTickets.Consume(reply.Ticket); ok {
				t.Fatal("native ticket reused")
			}
		}
	}
}

func TestNativeRuntimeLogTicketUpgradeRolePathAndTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := "/ws/pipelines/literal%3F/runs/r:one/stages/รัน/runtime-logs"
	escaped := "/ws/pipelines/literal%253F/runs/r:one/stages/" + url.PathEscape("รัน") + "/runtime-logs"
	for _, tc := range []struct {
		name, role, path string
		ttl              time.Duration
		code             int
	}{{"valid", "operator", path, time.Minute, 204}, {"viewer", "viewer", path, time.Minute, 403}, {"other-stage", "operator", strings.Replace(path, "รัน", "foreign", 1), time.Minute, 403}, {"expired", "admin", path, 0, 401}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{wsTickets: newWSTicketStore(tc.ttl)}
			ticket, _, err := s.wsTickets.Issue(grantFor("subject", tc.path, tc.role))
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.GET("/ws/pipelines/:pipelineId/runs/:runId/stages/:stageId/runtime-logs", s.wsTicketGate(), func(c *gin.Context) { c.Status(204) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, escaped+"?ticket="+url.QueryEscape(ticket), nil))
			if w.Code != tc.code {
				t.Fatal("native upgrade gate", w.Code, w.Body.String())
			}
			w = httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, escaped+"?ticket="+url.QueryEscape(ticket), nil))
			if w.Code != 401 {
				t.Fatal("ticket was not single-use")
			}
		})
	}
}

func TestNativeRuntimeLogErrorMappingIsFixed(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{service.ErrRuntimeLogRequest, 400}, {service.ErrRuntimeLogExpired, 404}, {service.ErrRuntimeLogCapacity, 429}, {service.ErrNativeRuntimeUnavailable, 503}} {
		code, message := nativeRuntimeLogError(tc.err)
		if code != tc.code || strings.Contains(message, "native runtime") {
			t.Fatal("unexpected native WS fixed mapping", code, message)
		}
	}
}
