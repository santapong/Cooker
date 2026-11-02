package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestSourceTokenReplacementRegisteredRouteRetainsAdminAndMFA(t *testing.T) {
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
	for _, mfa := range []bool{false, true} {
		cfg := &config.Config{}
		if mfa {
			cfg.OIDC.MFAACRValues = []string{"mfa"}
		}
		r := gin.New()
		s := &Server{router: r, handler: handler.New(memory.New(), nil, nil), config: cfg, oidcMW: mw}
		s.registerRoutes()
		for _, role := range []string{"", "viewer", "operator", "admin"} {
			token := ""
			if role != "" {
				token, _, err = issuer.Issue(role, role+"@fixture.invalid", role, role)
				if err != nil {
					t.Fatal(err)
				}
			}
			req := httptest.NewRequest("POST", "/api/v1/admin/source-connections/owned/token-replacement", strings.NewReader(`{"version":1,"token":"synthetic-new-token"}`))
			req.Header.Set("Content-Type", "application/json")
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			want := 403
			if role == "" {
				want = 401
			} else if role == "admin" && !mfa {
				want = 503
			}
			if w.Code != want {
				t.Fatalf("role=%s mfa=%v status=%d want=%d", role, mfa, w.Code, want)
			}
			if strings.Contains(w.Body.String(), "synthetic-new-token") {
				t.Fatal("route denial leaked submitted token")
			}
		}
	}
}
