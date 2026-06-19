package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
)

func grantFor(sub, path string, roles ...string) ticketGrant {
	return ticketGrant{Subject: sub, Roles: roles, Path: path}
}

func TestWSTicketStore_IssueAndConsume(t *testing.T) {
	s := newWSTicketStore(60 * time.Second)
	g := grantFor("alice", "/ws/pipeline-run/r1", "operator")
	tok, exp, err := s.Issue(g)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok == "" {
		t.Fatal("empty ticket")
	}
	if !exp.After(time.Now()) {
		t.Errorf("expiry %v not in future", exp)
	}

	got, ok := s.Consume(tok)
	if !ok {
		t.Fatal("Consume rejected fresh ticket")
	}
	if got.Subject != "alice" {
		t.Errorf("subject = %q, want alice", got.Subject)
	}
	if got.Path != "/ws/pipeline-run/r1" {
		t.Errorf("path = %q, want /ws/pipeline-run/r1", got.Path)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "operator" {
		t.Errorf("roles = %v, want [operator]", got.Roles)
	}
}

func TestWSTicketStore_SingleUse(t *testing.T) {
	s := newWSTicketStore(60 * time.Second)
	tok, _, _ := s.Issue(grantFor("alice", "/ws/pipeline-run/r1"))

	if _, ok := s.Consume(tok); !ok {
		t.Fatal("first consume should succeed")
	}
	if _, ok := s.Consume(tok); ok {
		t.Error("second consume should fail (single-use)")
	}
}

func TestWSTicketStore_Expires(t *testing.T) {
	s := newWSTicketStore(0) // zero TTL — every ticket is already expired
	tok, _, _ := s.Issue(grantFor("alice", "/ws/pipeline-run/r1"))

	// Sleep 1ms to be safe past the expiry boundary.
	time.Sleep(time.Millisecond)
	if _, ok := s.Consume(tok); ok {
		t.Error("expired ticket should be rejected")
	}
}

func TestWSTicketStore_UnknownToken(t *testing.T) {
	s := newWSTicketStore(60 * time.Second)
	if _, ok := s.Consume("not-a-real-ticket"); ok {
		t.Error("unknown token should be rejected")
	}
}

func TestWSTicketStore_TokensAreUnique(t *testing.T) {
	s := newWSTicketStore(60 * time.Second)
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, _, _ := s.Issue(grantFor("alice", "/ws/pipeline-run/r1"))
		if seen[tok] {
			t.Fatalf("duplicate ticket on iteration %d", i)
		}
		seen[tok] = true
	}
}

func TestMatchWSPath(t *testing.T) {
	good := []string{
		"/ws/pipeline-run/abc",
		"/ws/app-run/abc",
		"/ws/docker/build/b1",
		"/ws/runs/r1/stages/s1/logs",
		"/ws/runtime/app1/svc1/logs",
		"/ws/kubernetes/watch",
	}
	for _, p := range good {
		if !matchWSPath(p) {
			t.Errorf("matchWSPath(%q) = false, want true", p)
		}
	}
	bad := []string{
		"/ws/pipeline-run/", // empty param segment
		"/ws/pipeline-run",  // missing segment
		"/ws/evil",
		"/ws/kubernetes/watch/extra",
		"/api/v1/pipelines",
		"",
	}
	for _, p := range bad {
		if matchWSPath(p) {
			t.Errorf("matchWSPath(%q) = true, want false", p)
		}
	}
}

func TestRouteRoleRequirement(t *testing.T) {
	if got := routeRoleRequirement("/ws/pipeline-run/r1"); got != nil {
		t.Errorf("pipeline-run should be read-level, got %v", got)
	}
	if got := routeRoleRequirement("/ws/kubernetes/watch"); len(got) == 0 {
		t.Error("kubernetes/watch should require a write role")
	}
	if got := routeRoleRequirement("/ws/runtime/app1/svc1/logs"); len(got) == 0 {
		t.Error("runtime logs should require a write role")
	}
}

// --- mint endpoint tests ---------------------------------------------

func newMintServer() *Server {
	return &Server{wsTickets: newWSTicketStore(60 * time.Second)}
}

// mintCtx builds a gin context with the given claims and JSON body
// pointed at the mint handler, returning the recorder.
func mintRequest(t *testing.T, s *Server, claims *auth.Claims, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/ws-tickets", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if claims != nil {
		c.Set("user", claims)
	}
	s.issueWSTicket(c)
	return w
}

func TestIssueWSTicket_RequiresAuth(t *testing.T) {
	s := newMintServer()
	w := mintRequest(t, s, nil, `{"path":"/ws/pipeline-run/r1"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestIssueWSTicket_RejectsMissingPath(t *testing.T) {
	s := newMintServer()
	claims := &auth.Claims{Subject: "alice", Roles: []string{"viewer"}}
	w := mintRequest(t, s, claims, `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestIssueWSTicket_RejectsUnknownPath(t *testing.T) {
	s := newMintServer()
	claims := &auth.Claims{Subject: "alice", Roles: []string{"admin"}}
	w := mintRequest(t, s, claims, `{"path":"/ws/evil/path"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestIssueWSTicket_ViewerAllowedReadRoute(t *testing.T) {
	s := newMintServer()
	claims := &auth.Claims{Subject: "alice", Roles: []string{"viewer"}}
	w := mintRequest(t, s, claims, `{"path":"/ws/pipeline-run/r1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (viewer may read run logs)", w.Code)
	}
	var body struct {
		Ticket string `json:"ticket"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Ticket == "" {
		t.Fatal("expected a ticket in the response")
	}
}

func TestIssueWSTicket_ViewerDeniedKubeWatch(t *testing.T) {
	s := newMintServer()
	claims := &auth.Claims{Subject: "viewer-bob", Roles: []string{"viewer"}}
	w := mintRequest(t, s, claims, `{"path":"/ws/kubernetes/watch"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (viewer must not watch the cluster)", w.Code)
	}
}

func TestIssueWSTicket_ViewerDeniedRuntimeLogs(t *testing.T) {
	s := newMintServer()
	claims := &auth.Claims{Subject: "viewer-bob", Roles: []string{"viewer"}}
	w := mintRequest(t, s, claims, `{"path":"/ws/runtime/app1/svc1/logs"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (viewer must not read runtime logs)", w.Code)
	}
}

func TestIssueWSTicket_OperatorAllowedKubeWatch(t *testing.T) {
	s := newMintServer()
	claims := &auth.Claims{Subject: "op", Roles: []string{"operator"}}
	w := mintRequest(t, s, claims, `{"path":"/ws/kubernetes/watch"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (operator may watch)", w.Code)
	}
}

// --- gate tests -------------------------------------------------------

// gateRequest runs the wsTicketGate middleware against a request to
// reqPath carrying ?ticket=tok and returns whether the request was
// allowed through (c.Next reached) plus the recorder.
func gateRequest(t *testing.T, s *Server, reqPath, tok string) (bool, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	url := reqPath
	if tok != "" {
		url += "?ticket=" + tok
	}
	c.Request = httptest.NewRequest(http.MethodGet, url, nil)
	passed := false
	gate := s.wsTicketGate()
	gate(c)
	if !c.IsAborted() {
		passed = true
	}
	return passed, w
}

func TestWSTicketGate_RejectsMissingTicket(t *testing.T) {
	s := newMintServer()
	passed, w := gateRequest(t, s, "/ws/pipeline-run/r1", "")
	if passed {
		t.Fatal("gate allowed a request with no ticket")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestWSTicketGate_AllowsMatchingScope(t *testing.T) {
	s := newMintServer()
	tok, _, _ := s.wsTickets.Issue(grantFor("alice", "/ws/pipeline-run/r1", "viewer"))
	passed, _ := gateRequest(t, s, "/ws/pipeline-run/r1", tok)
	if !passed {
		t.Fatal("gate rejected a correctly-scoped ticket")
	}
}

// The core IDOR test: a ticket minted for run r1 must not work for r2.
func TestWSTicketGate_RejectsResourceMismatch(t *testing.T) {
	s := newMintServer()
	tok, _, _ := s.wsTickets.Issue(grantFor("alice", "/ws/pipeline-run/r1", "viewer"))
	passed, w := gateRequest(t, s, "/ws/pipeline-run/r2", tok)
	if passed {
		t.Fatal("gate allowed cross-resource ticket replay (IDOR)")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

// A ticket consumed once cannot be replayed against the same resource.
func TestWSTicketGate_SingleUse(t *testing.T) {
	s := newMintServer()
	tok, _, _ := s.wsTickets.Issue(grantFor("alice", "/ws/pipeline-run/r1", "viewer"))
	if passed, _ := gateRequest(t, s, "/ws/pipeline-run/r1", tok); !passed {
		t.Fatal("first use should pass")
	}
	if passed, _ := gateRequest(t, s, "/ws/pipeline-run/r1", tok); passed {
		t.Fatal("second use should fail (single-use)")
	}
}

// Defence in depth: even a forged ticket carrying only viewer roles is
// rejected by the gate's role check on a write-gated route.
func TestWSTicketGate_RejectsInsufficientRole(t *testing.T) {
	s := newMintServer()
	tok, _, _ := s.wsTickets.Issue(grantFor("viewer-bob", "/ws/kubernetes/watch", "viewer"))
	passed, w := gateRequest(t, s, "/ws/kubernetes/watch", tok)
	if passed {
		t.Fatal("gate allowed viewer onto kubernetes/watch")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestWSTicketGate_AllowsOperatorWriteRoute(t *testing.T) {
	s := newMintServer()
	tok, _, _ := s.wsTickets.Issue(grantFor("op", "/ws/kubernetes/watch", "operator"))
	passed, _ := gateRequest(t, s, "/ws/kubernetes/watch", tok)
	if !passed {
		t.Fatal("gate rejected operator on kubernetes/watch")
	}
}

func TestWSTicketGate_SetsContextIdentity(t *testing.T) {
	s := newMintServer()
	tok, _, _ := s.wsTickets.Issue(grantFor("alice", "/ws/pipeline-run/r1", "operator"))
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/ws/pipeline-run/r1?ticket="+tok, nil)
	s.wsTicketGate()(c)
	if c.IsAborted() {
		t.Fatal("gate aborted a valid request")
	}
	if got, _ := c.Get("ws-subject"); got != "alice" {
		t.Errorf("ws-subject = %v, want alice", got)
	}
	if _, ok := c.Get("ws-roles"); !ok {
		t.Error("ws-roles not set in context")
	}
}
