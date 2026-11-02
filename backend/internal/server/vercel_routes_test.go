package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/provider/vercel"
)

type vercelFixture struct{}

func (vercelFixture) Verify(context.Context, string, string) (vercel.Identity, error) {
	return vercel.Identity{ID: "user_fixture", Name: "Fixture", Scope: "personal"}, nil
}
func (vercelFixture) Projects(context.Context, string, string) ([]vercel.Project, error) {
	return []vercel.Project{{ID: "prj_fixture", Name: "fixture"}}, nil
}
func (vercelFixture) Deployments(context.Context, string, string, string) ([]vercel.Deployment, error) {
	return []vercel.Deployment{{ID: "dpl_fixture", ProjectID: "prj_fixture", Name: "fixture", State: "READY", Target: "production", URL: "https://fixture.vercel.app", CreatedAt: time.Now().UTC()}}, nil
}
func TestVercelRegisteredRoutesAuthenticateAndSync(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := local.NewIssuer(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := issuer.Issue("admin-fixture", "admin@fixture.invalid", "Admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, _, err := issuer.Issue("viewer-fixture", "viewer@fixture.invalid", "Viewer", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	h := &handler.Handler{Vercel: &vercel.Service{Store: vercel.NewMemory(), Codec: codec, API: vercelFixture{}}}
	makeRouter := func(mfa bool) *gin.Engine {
		middleware, err := auth.NewMiddleware(ctx, config.OIDCConfig{})
		if err != nil {
			t.Fatal(err)
		}
		middleware.EnableLocalAuth(issuer)
		cfg := &config.Config{}
		if mfa {
			cfg.OIDC.MFAACRValues = []string{"mfa"}
		}
		router := gin.New()
		s := &Server{router: router, handler: h, config: cfg, oidcMW: middleware}
		s.registerRoutes()
		return router
	}
	router := makeRouter(false)
	send := func(r *gin.Engine, token, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), "secret-fixture-token") {
			t.Fatal("credential leaked")
		}
		return w
	}
	base := "/api/v1/admin/integrations/vercel/connections"
	for _, path := range []string{"/api/v1/admin/integrations/vercel/landing/review", "/api/v1/admin/integrations/vercel/landing/operations/example/deploy", "/api/v1/admin/integrations/vercel/landing/operations/example/check"} {
		if w := send(router, viewerToken, "POST", path, `{}`); w.Code != 403 {
			t.Fatal("viewer admitted landing mutation", path, w.Code)
		}
		if w := send(makeRouter(true), adminToken, "POST", path, `{}`); w.Code != 403 {
			t.Fatal("landing mutation omitted MFA", path, w.Code)
		}
		if w := send(router, adminToken, "POST", path, `{}`); w.Code != 503 {
			t.Fatal("landing mutation enabled without durable publisher", path, w.Code)
		}
	}
	if w := send(router, "", "GET", "/api/v1/integrations/vercel/landing", ""); w.Code != 401 {
		t.Fatal("landing history needs authentication", w.Code)
	}
	if w := send(router, "", "GET", "/landing-preview/", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Cooker") {
		t.Fatal("fixed public landing missing", w.Code)
	}
	if w := send(router, "", "GET", "/landing-preview/SOURCE.json", ""); w.Code != 404 {
		t.Fatal("private manifest exposed", w.Code)
	}
	if w := send(router, "", "GET", "/api/v1/integrations/vercel", ""); w.Code != 401 {
		t.Fatal("unauthenticated read admitted", w.Code)
	}
	if w := send(router, viewerToken, "POST", base, `{}`); w.Code != 403 {
		t.Fatal("viewer admitted connection mutation", w.Code)
	}
	if w := send(makeRouter(true), adminToken, "POST", base, `{}`); w.Code != 403 {
		t.Fatal("registered admin route omitted MFA", w.Code)
	}
	w := send(router, adminToken, "POST", base, `{"name":"Vercel fixture","token":"secret-fixture-token"}`)
	if w.Code != http.StatusCreated {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Connection vercel.Connection `json:"connection"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	id := result.Connection.ID
	w = send(router, adminToken, "PUT", base+"/"+id, `{"version":1,"projectIds":["prj_fixture"]}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = send(router, adminToken, "POST", base+"/"+id+"/sync", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = send(router, viewerToken, "GET", "/api/v1/integrations/vercel", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "dpl_fixture") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = send(router, viewerToken, "POST", base+"/"+id+"/sync", "")
	if w.Code != 403 {
		t.Fatal("viewer synced account", w.Code)
	}
	w = send(router, adminToken, "DELETE", base+"/"+id+"?version=2", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = send(router, viewerToken, "GET", "/api/v1/integrations/vercel", "")
	if strings.Contains(w.Body.String(), "dpl_fixture") {
		t.Fatal("disconnect retained imported data")
	}
}
func TestVercelBootRequiresDurabilityAndEncryption(t *testing.T) {
	svc, owned, err := bootVercel(context.Background(), &config.Config{}, &crypto.Codec{}, nil)
	if svc != nil || owned != nil || err != nil {
		t.Fatal("unconfigured integration enabled")
	}
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	svc, owned, err = bootVercel(context.Background(), &config.Config{}, codec, nil)
	if svc != nil || owned != nil || err != nil {
		t.Fatal("non-durable connection enabled")
	}
}
