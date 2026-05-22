package governance_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/governance"
)

func TestClient_Disabled_ReturnsAllow(t *testing.T) {
	c := governance.New("", nil, nil)
	if c.Enabled() {
		t.Fatal("client with empty URL should not be enabled")
	}
	d, err := c.Authorize(context.Background(), "tok", "svc", "prod", "req-1")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !d.Allowed() {
		t.Errorf("decision = %v, want allow", d)
	}
}

func TestClient_BootstrapBypass(t *testing.T) {
	c := governance.New("http://unreachable.invalid", []string{"governance"}, nil)
	d, err := c.Authorize(context.Background(), "tok", "Governance", "prod", "req-1")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !d.Allowed() {
		t.Errorf("bootstrap service should be allowed; got %v", d)
	}
	if d.PolicyID != "cooker.governance.bootstrap" {
		t.Errorf("policy_id = %q", d.PolicyID)
	}
}

func TestClient_DenyPassesThroughReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(governance.Decision{
			Decision: "deny",
			Reason:   "actor not in any prod-deployer group for svc-x",
			PolicyID: "rule.prod.human",
			AuditID:  "audit-42",
		})
	}))
	defer srv.Close()

	c := governance.New(srv.URL, nil, nil)
	d, err := c.Authorize(context.Background(), "tok", "svc-x", "prod", "req-1")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if d.Allowed() {
		t.Fatal("expected deny")
	}
	if !strings.Contains(d.Reason, "prod-deployer group") {
		t.Errorf("reason did not carry through: %q", d.Reason)
	}
	if d.AuditID != "audit-42" {
		t.Errorf("audit id = %q", d.AuditID)
	}
}

func TestClient_AllowDecodesCleanly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(governance.Decision{Decision: "allow", PolicyID: "rule.nonprod"})
	}))
	defer srv.Close()

	c := governance.New(srv.URL, nil, nil)
	d, err := c.Authorize(context.Background(), "tok", "svc-x", "dev", "req-1")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !d.Allowed() {
		t.Errorf("decision = %v", d)
	}
}

func TestClient_TransportError_FailOpenEnv(t *testing.T) {
	c := governance.New("http://127.0.0.1:1/this-port-is-not-listening", nil, []string{"dev"})
	d, err := c.Authorize(context.Background(), "tok", "svc-x", "dev", "req-1")
	if err != nil {
		t.Fatalf("dev should be fail-open; got err %v", err)
	}
	if !d.Allowed() {
		t.Errorf("decision = %v", d)
	}
	if !strings.Contains(d.Reason, "unreachable") {
		t.Errorf("reason did not mark unreachable: %q", d.Reason)
	}
}

func TestClient_TransportError_FailClosedEnv(t *testing.T) {
	c := governance.New("http://127.0.0.1:1/this-port-is-not-listening", nil, []string{"dev"})
	_, err := c.Authorize(context.Background(), "tok", "svc-x", "prod", "req-1")
	if err == nil {
		t.Fatal("prod should be fail-closed when transport fails")
	}
	if !errors.Is(err, governance.ErrGovernanceUnreachable) {
		t.Errorf("err = %v, want ErrGovernanceUnreachable joined", err)
	}
}

func TestClient_PassesContextAndPayload(t *testing.T) {
	var got struct {
		Actor struct {
			Token string `json:"token"`
		} `json:"actor"`
		Action   string `json:"action"`
		Resource struct {
			Service string `json:"service"`
			Env     string `json:"env"`
		} `json:"resource"`
		Context struct {
			RequestID string `json:"request_id"`
		} `json:"context"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(governance.Decision{Decision: "allow"})
	}))
	defer srv.Close()

	c := governance.New(srv.URL, nil, nil)
	_, _ = c.Authorize(context.Background(), "alice-tok", "svc-x", "prod", "req-99")
	if got.Actor.Token != "alice-tok" {
		t.Errorf("token = %q", got.Actor.Token)
	}
	if got.Action != "deploy" {
		t.Errorf("action = %q", got.Action)
	}
	if got.Resource.Service != "svc-x" || got.Resource.Env != "prod" {
		t.Errorf("resource = %+v", got.Resource)
	}
	if got.Context.RequestID != "req-99" {
		t.Errorf("request_id = %q", got.Context.RequestID)
	}
}

func TestClient_CallerToken_AttachesAuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(governance.Decision{Decision: "allow"})
	}))
	defer srv.Close()

	c := governance.New(srv.URL, nil, nil).WithCallerToken("svc-token-abc")
	_, _ = c.Authorize(context.Background(), "actor-tok", "svc-x", "prod", "req-1")
	if gotAuth != "Bearer svc-token-abc" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer svc-token-abc")
	}
}

func TestClient_NoCallerToken_OmitsAuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(governance.Decision{Decision: "allow"})
	}))
	defer srv.Close()

	c := governance.New(srv.URL, nil, nil) // no WithCallerToken
	_, _ = c.Authorize(context.Background(), "actor-tok", "svc-x", "prod", "req-1")
	if gotAuth != "" {
		t.Fatalf("Authorization header = %q, want empty", gotAuth)
	}
}

func TestClient_AuthorizeOnBehalf_AttachesDelegateToken(t *testing.T) {
	var gotAuth string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(governance.Decision{Decision: "allow"})
	}))
	defer srv.Close()

	c := governance.New(srv.URL, nil, nil).WithDelegateToken("delegate-tok")
	_, err := c.AuthorizeOnBehalf(context.Background(),
		governance.PreresolvedActor{Kind: "human", ID: "alice", Groups: []string{"prod-deployers"}},
		"svc-x", "prod", "run-1")
	if err != nil {
		t.Fatalf("authorize on behalf: %v", err)
	}
	if gotAuth != "Bearer delegate-tok" {
		t.Errorf("Authorization = %q, want Bearer delegate-tok", gotAuth)
	}
	if !strings.Contains(string(gotBody), `"preresolved"`) {
		t.Errorf("body missing preresolved field: %s", gotBody)
	}
	if !strings.Contains(string(gotBody), `"alice"`) {
		t.Errorf("body missing actor id: %s", gotBody)
	}
}

func TestClient_AuthorizeOnBehalf_NoDelegateToken_IsNoOp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called when DelegateToken is unset")
	}))
	defer srv.Close()

	c := governance.New(srv.URL, nil, nil) // no WithDelegateToken
	d, err := c.AuthorizeOnBehalf(context.Background(),
		governance.PreresolvedActor{Kind: "human", ID: "alice"},
		"svc-x", "prod", "run-1")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !d.Allowed() {
		t.Errorf("decision = %+v, want allow (delegation disabled fallback)", d)
	}
	if d.PolicyID != "cooker.governance.delegation_disabled" {
		t.Errorf("policy_id = %q, want cooker.governance.delegation_disabled", d.PolicyID)
	}
}

func TestClient_AuthorizeOnBehalf_BootstrapBypass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called for bootstrap services")
	}))
	defer srv.Close()

	c := governance.New(srv.URL, []string{"governance"}, nil).WithDelegateToken("tok")
	d, err := c.AuthorizeOnBehalf(context.Background(),
		governance.PreresolvedActor{Kind: "human", ID: "alice"},
		"governance", "prod", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed() || d.PolicyID != "cooker.governance.bootstrap" {
		t.Errorf("decision = %+v, want bootstrap allow", d)
	}
}
