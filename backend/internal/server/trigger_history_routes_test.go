package server

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestTriggerHistoryRegisteredAdminReadRolesAndFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	issuer, err := local.NewIssuer(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mw, err := auth.NewMiddleware(ctx, config.OIDCConfig{})
	if err != nil {
		t.Fatal(err)
	}
	mw.EnableLocalAuth(issuer)
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	h := handler.New(st, codec, nil)
	h.Automation = &service.Automation{Triggers: &service.TriggerService{Store: memory.NewTriggerStore()}}
	r := gin.New()
	s := &Server{router: r, handler: h, config: &config.Config{}, oidcMW: mw}
	s.registerRoutes()
	send := func(token, path string) int {
		req := httptest.NewRequest("GET", "/api/v1/admin"+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	for _, role := range []string{"", "viewer", "operator", "admin"} {
		token := ""
		if role != "" {
			token, _, err = issuer.Issue(role, role+"@fixture.invalid", role, role)
			if err != nil {
				t.Fatal(err)
			}
		}
		want := 403
		if role == "" {
			want = 401
		}
		if role == "admin" {
			want = 200
		}
		if code := send(token, "/trigger-deliveries"); code != want {
			t.Fatalf("role %q list=%d want %d", role, code, want)
		}
		if code := send(token, "/webhook-attempts"); code != want {
			t.Fatalf("role %q audit=%d want %d", role, code, want)
		}
		if role != "admin" {
			if code := send(token, "/trigger-deliveries/missing/history"); code != want {
				t.Fatalf("role %q detail=%d", role, code)
			}
		} else {
			for _, path := range []string{"/trigger-deliveries?limit=101", "/trigger-deliveries?offset=-1", "/trigger-deliveries?limit=oops", "/trigger-deliveries?status=provider-secret", "/trigger-deliveries?provider=vercel", "/trigger-deliveries?appId=vapp_imported"} {
				if code := send(token, path); code != 400 {
					t.Fatalf("%s=%d", path, code)
				}
			}
			if code := send(token, "/trigger-deliveries/missing/history"); code != 404 {
				t.Fatal(code)
			}
			for _, path := range []string{"/webhook-attempts?limit=101", "/webhook-attempts?status=stale", "/webhook-attempts?appId=vapp_imported"} {
				if code := send(token, path); code != 400 {
					t.Fatal(path, code)
				}
			}
		}
	}
	// The routes inherit configured MFA; an ordinary local admin token cannot
	// bypass a step-up requirement merely because these endpoints are reads.
	mfaRouter := gin.New()
	mfaConfig := &config.Config{}
	mfaConfig.OIDC.MFAACRValues = []string{"mfa"}
	(&Server{router: mfaRouter, handler: h, config: mfaConfig, oidcMW: mw}).registerRoutes()
	admin, _, _ := issuer.Issue("admin", "admin@fixture.invalid", "Admin", "admin")
	req := httptest.NewRequest("GET", "/api/v1/admin/trigger-deliveries", nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	w := httptest.NewRecorder()
	mfaRouter.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("MFA bypass", w.Code)
	}
	runs, _ := st.Runs.List(ctx, "", 0, 0)
	if len(runs) != 0 {
		t.Fatal("history read created execution")
	}
}
