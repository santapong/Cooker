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
	"github.com/santapong/cooker/internal/store/memory"
)

func TestNotificationHistoryRegisteredAdminRoleAndMFA(t *testing.T) {
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
	h := handler.New(memory.New(), codec, nil)
	for _, requireMFA := range []bool{false, true} {
		cfg := &config.Config{}
		if requireMFA {
			cfg.OIDC.MFAACRValues = []string{"mfa"}
		}
		r := gin.New()
		(&Server{router: r, handler: h, config: cfg, oidcMW: mw}).registerRoutes()
		for _, role := range []string{"", "viewer", "operator", "admin"} {
			for _, path := range []string{"/api/v1/admin/notification-deliveries", "/api/v1/admin/notification-deliveries/one"} {
				request := httptest.NewRequest("GET", path, nil)
				if role != "" {
					token, _, err := issuer.Issue(role, role+"@fixture.invalid", role, role)
					if err != nil {
						t.Fatal(err)
					}
					request.Header.Set("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, request)
				want := 403
				if role == "" {
					want = 401
				}
				if role == "admin" && !requireMFA {
					want = 503
				}
				if w.Code != want {
					t.Fatalf("role=%s mfa=%t path=%s status=%d want=%d", role, requireMFA, path, w.Code, want)
				}
			}
		}
	}
}
