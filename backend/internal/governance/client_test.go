package governance_test

import (
	"context"
	"encoding/json"
	"errors"
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
