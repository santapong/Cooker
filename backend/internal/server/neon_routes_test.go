package server

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/audit"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/provider/neon"
)

type neonRegisteredFixture struct{ calls int }

func (f *neonRegisteredFixture) Projects(_ context.Context, _ string, org string) (*neon.ProjectInventory, error) {
	f.calls++
	now := time.Now().UTC()
	return &neon.ProjectInventory{OrganizationID: org, Complete: true, ObservedAt: now, Projects: []neon.Project{{ID: "project-one", Name: "Fixture", OrganizationID: org, RegionID: "aws-us-east-2", CreatedAt: now, UpdatedAt: now}}}, nil
}
func (f *neonRegisteredFixture) Branches(_ context.Context, _ string, project string) (*neon.BranchInventory, error) {
	f.calls++
	now := time.Now().UTC()
	return &neon.BranchInventory{ProjectID: project, Complete: true, ObservedAt: now, Branches: []neon.Branch{{ID: "br-one", ProjectID: project, Name: "main", CreatedAt: now, UpdatedAt: now}}}, nil
}
func (f *neonRegisteredFixture) Databases(_ context.Context, _ string, project, branch string) (*neon.DatabaseInventory, error) {
	f.calls++
	return &neon.DatabaseInventory{ProjectID: project, BranchID: branch, Complete: true, ObservedAt: time.Now().UTC(), Databases: []neon.Database{{ID: "123", ProjectID: project, BranchID: branch, Name: "fixture"}}}, nil
}
func (f *neonRegisteredFixture) Endpoints(_ context.Context, _ string, project string) (*neon.EndpointInventory, error) {
	f.calls++
	return &neon.EndpointInventory{ProjectID: project, Complete: true, ObservedAt: time.Now().UTC(), Endpoints: []neon.Endpoint{{ID: "ep-fixture", ProjectID: project, BranchID: "br-one", Type: "read_write", CurrentState: "active"}}}, nil
}

func TestNeonActualRegisteredRoutesRolesMFAAndEncryptedJourney(t *testing.T) {
	gin.SetMode(gin.TestMode)
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := local.NewIssuer(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, role := range []string{"admin", "viewer", "operator"} {
		token, _, err := issuer.Issue(role+"-fixture", role+"@fixture.invalid", role, role)
		if err != nil {
			t.Fatal(err)
		}
		tokens[role] = token
	}
	provider := &neonRegisteredFixture{}
	sink := &recordingSink{}
	h := &handler.Handler{Neon: &neon.ConnectionService{Store: neon.NewMemoryConnections(), Codec: codec, API: provider}}
	makeRouter := func(mfa bool) *gin.Engine {
		mw, err := auth.NewMiddleware(context.Background(), config.OIDCConfig{})
		if err != nil {
			t.Fatal(err)
		}
		mw.EnableLocalAuth(issuer)
		cfg := &config.Config{}
		if mfa {
			cfg.OIDC.MFAACRValues = []string{"mfa"}
		}
		r := gin.New()
		(&Server{router: r, handler: h, config: cfg, oidcMW: mw, audit: sink}).registerRoutes()
		return r
	}
	r, mfaRouter := makeRouter(false), makeRouter(true)
	const secret = "synthetic-neon-registered-route-credential"
	call := func(router *gin.Engine, token, method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("registered %s %s status %d; want %d", method, path, w.Code, want)
		}
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("registered response leaked credential")
		}
		return w
	}
	const base = "/api/v1/admin/integrations/neon/connections"
	const reads = "/api/v1/integrations/neon"
	for _, path := range []string{reads, reads + "/connections/fixture", "/api/v1/capabilities"} {
		call(r, "", "GET", path, "", 401)
	}
	protected := []struct{ method, path string }{
		{"POST", base}, {"PUT", base + "/fixture"}, {"DELETE", base + "/fixture?version=1"}, {"POST", base + "/fixture/sync"},
		{"GET", base + "/fixture/projects?version=1"},
		{"GET", base + "/fixture/projects/project-one/endpoints?version=1"},
		{"GET", base + "/fixture/projects/project-one/branches/br-one/databases?version=1"},
	}
	for _, endpoint := range protected {
		call(r, "", endpoint.method, endpoint.path, `{}`, 401)
		for _, role := range []string{"viewer", "operator"} {
			call(r, tokens[role], endpoint.method, endpoint.path, `{}`, 403)
		}
		call(mfaRouter, tokens["admin"], endpoint.method, endpoint.path, `{}`, 403)
	}
	if provider.calls != 0 {
		t.Fatal("denied registered route contacted provider")
	}
	w := call(r, tokens["admin"], "POST", base, `{"name":"Registered fixture","organizationId":"org-fixture","accountMode":"unknown","apiKey":"`+secret+`","projectIds":[],"acknowledgeKeyPermissions":true}`, 201)
	var created struct{ Connection neon.Connection }
	if json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Connection.ID == "" || created.Connection.Version != 1 {
		t.Fatal("registered create receipt missing")
	}
	id := created.Connection.ID
	w = call(r, tokens["admin"], "GET", base+"/"+id+"/projects?version=1", "", 200)
	if !strings.Contains(w.Body.String(), `"project-one"`) || !strings.Contains(w.Body.String(), `"selectionChanged":false`) {
		t.Fatal("project discovery changed selection or omitted safe inventory")
	}
	call(r, tokens["admin"], "PUT", base+"/"+id, `{"version":1,"projectIds":["project-one"]}`, 200)
	w = call(r, tokens["admin"], "POST", base+"/"+id+"/sync", "", 200)
	if !strings.Contains(w.Body.String(), `"state":"observed"`) {
		t.Fatal("registered sync not observed")
	}
	w = call(r, tokens["viewer"], "GET", reads+"/connections/"+id, "", 200)
	if !strings.Contains(w.Body.String(), `"br-one"`) || strings.Contains(w.Body.String(), `"credential":`) {
		t.Fatal("safe snapshot missing/private bytes exposed")
	}
	for _, path := range []string{base + "/" + id + "/projects/project-one/endpoints?version=2", base + "/" + id + "/projects/project-one/branches/br-one/databases?version=2"} {
		w = call(r, tokens["admin"], "GET", path, "", 200)
		if !strings.Contains(w.Body.String(), `"databaseHealth":"not_checked"`) {
			t.Fatal("resource read fabricated health")
		}
	}
	w = call(r, tokens["viewer"], "GET", "/api/v1/capabilities", "", 200)
	if !strings.Contains(w.Body.String(), `"neonMetadata":true`) {
		t.Fatal("registered capability absent")
	}
	call(r, tokens["admin"], "PUT", base+"/"+id, `{"version":2,"enabled":false}`, 200)
	before := provider.calls
	call(r, tokens["admin"], "POST", base+"/"+id+"/sync", "", 422)
	if provider.calls != before {
		t.Fatal("paused route contacted provider")
	}
	call(r, tokens["admin"], "DELETE", base+"/"+id+"?version=2", "", 409)
	call(r, tokens["admin"], "DELETE", base+"/"+id+"?version=3", "", 200)
	call(r, tokens["viewer"], "GET", reads+"/connections/"+id, "", 404)
	data, err := json.Marshal(sink.snapshot())
	if err != nil || strings.Contains(string(data), secret) {
		t.Fatal("audit emitted credential", err)
	}
	found := false
	for _, event := range sink.snapshot() {
		if event.Path == base && event.Method == "POST" && event.Status == 201 && event.UserSub == "admin-fixture" {
			found = true
		}
	}
	if !found || !audit.IsRedacted(base) || !audit.IsRedacted(base+"/:id") {
		t.Fatal("registered audit metadata/redaction contract absent")
	}
	h.Neon = nil
	w = call(r, tokens["viewer"], "GET", "/api/v1/capabilities", "", 200)
	if !strings.Contains(w.Body.String(), `"neonMetadata":false`) {
		t.Fatal("disabled capability remained active")
	}
}

func TestNeonBootReusesDatabaseWithoutCallsAndRequiresCodec(t *testing.T) {
	shared, preferred := new(sql.DB), new(sql.DB) // inert handles: constructor must not ping/open/close them
	if bootNeon(nil, nil, shared) != nil || bootNeon(&crypto.Codec{}, nil, shared) != nil {
		t.Fatal("inactive encryption enabled Neon")
	}
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if bootNeon(codec, nil, nil) != nil {
		t.Fatal("missing durable store enabled Neon")
	}
	service := bootNeon(codec, nil, shared)
	if service == nil || service.Store.(*neon.PostgresConnections).DB != shared {
		t.Fatal("shared DB not reused")
	}
	service = bootNeon(codec, &jobQueueDeps{DB: preferred}, shared)
	if service == nil || service.Store.(*neon.PostgresConnections).DB != preferred {
		t.Fatal("job queue DB preference changed")
	}
}
